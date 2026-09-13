package goatcounter

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/marvinrabe/goatcounter/internal/log"
	"github.com/marvinrabe/goatcounter/internal/refspam"
	"zgo.at/json"
	"zgo.at/zdb"
	"zgo.at/zstd/zbool"
	"zgo.at/zstd/zint"
	"zgo.at/zstd/ztime"
	"zgo.at/zvalidate"
)

var (
	// Valid UUID for testing: 00112233-4455-6677-8899-aabbccddeeff
	TestSession    = zint.Uint128{0x11223344556677, 0x8899aabbccddeeff}
	TestSeqSession = zint.Uint128{TestSession[0], TestSession[1] + 1}
)

var (
	memlog     = log.Module("memstore")
	sesslog    = log.Module("session")
	refspamlog = log.Module("refspam")
)

type sessionKey string

type ms struct {
	hitMu sync.RWMutex
	hits  []Hit

	sessionMu     sync.RWMutex
	sessions      map[sessionKey]zint.Uint128          // sessionKey → sessionID
	sessionHashes map[zint.Uint128]sessionKey          // sessionID → sessionKey
	sessionPaths  map[zint.Uint128]map[PathID]struct{} // SessionID → path_id
	sessionSeen   map[zint.Uint128]int64               // SessionID → lastseen

	testHook bool
}

var Memstore ms

type storedSession struct {
	Sessions map[sessionKey]zint.Uint128          `json:"sessions"`
	Hashes   map[zint.Uint128]sessionKey          `json:"hashes"`
	Paths    map[zint.Uint128]map[PathID]struct{} `json:"paths"`
	Seen     map[zint.Uint128]int64               `json:"seen"`
}

type storedSessionRow struct {
	Key   string `db:"key"`
	Value []byte `db:"value"`
}

func (m *ms) Reset() {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()

	m.sessions = make(map[sessionKey]zint.Uint128)
	m.sessionHashes = make(map[zint.Uint128]sessionKey)
	m.sessionPaths = make(map[zint.Uint128]map[PathID]struct{})
	m.sessionSeen = make(map[zint.Uint128]int64)
	TestSeqSession = zint.Uint128{TestSession[0], TestSession[1] + 1}
}

// TestInit is like Init(), but enables the test hook to return sequential UUIDs
// instead of random ones.
func (m *ms) TestInit(db zdb.DB) error {
	m.testHook = true
	return m.Init(db)
}

func (m *ms) Init(db zdb.DB) error {
	m.hitMu.Lock()
	defer m.hitMu.Unlock()

	m.Reset()
	m.RestoreSessions(db)
	return nil
}

// RestoreSessions loads shutdown snapshots written by other processes. It is
// used both at startup and by the regular persistence job: in a rolling update
// the replacement process often starts before the old process gets a chance to
// write its final snapshot.
func (m *ms) RestoreSessions(db zdb.DB) {
	// A session snapshot is stored under a unique key. There may be more than
	// one when processes overlap (for example during a Kubernetes rolling
	// update), so load and merge every available snapshot. Deleting the exact
	// selected keys means a process which shuts down concurrently cannot have
	// its new snapshot deleted here. Multiple processes may restore the same
	// snapshot, which is desirable while they temporarily serve in parallel.
	var rows []storedSessionRow
	err := db.Select(context.Background(), &rows, `select key, value from store
		where key = 'session' or key like 'session:%'
		order by key`)
	if err != nil {
		memlog.Errorf(context.Background(), "load from DB store: %s", err)
		return
	}
	if len(rows) == 0 {
		memlog.Debugf(context.Background(), "no sessions stored in DB")
		return
	}

	stored := make([]storedSession, 0, len(rows))
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.Key)
		var snapshot storedSession
		if err := json.Unmarshal(row.Value, &snapshot); err != nil {
			memlog.Errorf(context.Background(), "unmarshal DB store %q: %s", row.Key, err)
			continue
		}
		stored = append(stored, snapshot)
	}
	if err := db.Exec(context.Background(), `delete from store where key in (:keys)`, map[string]any{"keys": keys}); err != nil {
		memlog.Errorf(context.Background(), "delete restored DB store: %s", err)
	}

	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	for _, snapshot := range stored {
		m.mergeSessions(snapshot)
	}
	memlog.Debug(context.Background(), "restored sessions from DB",
		"snapshots", len(stored),
		"sessions", len(m.sessions),
		"sessionHashes", len(m.sessionHashes),
		"sessionPaths", len(m.sessionPaths),
		"sessionSeen", len(m.sessionSeen))
}

// mergeSessions merges a shutdown snapshot into the in-memory session set.
// Overlapping processes can contain the same session under different IDs. The
// most recently seen ID wins, while paths from both copies are retained so a
// path is never incorrectly counted as a first visit after a restart.
//
// m.sessionMu must be held by the caller.
func (m *ms) mergeSessions(stored storedSession) {
	for key, id := range stored.Sessions {
		seen := stored.Seen[id]
		paths := stored.Paths[id]
		if paths == nil {
			paths = make(map[PathID]struct{})
		}

		current, ok := m.sessions[key]
		if !ok {
			m.sessions[key] = id
			m.sessionHashes[id] = key
			m.sessionSeen[id] = seen
			m.sessionPaths[id] = paths
			continue
		}

		currentPaths := m.sessionPaths[current]
		if currentPaths == nil {
			currentPaths = make(map[PathID]struct{})
		}
		for path := range paths {
			currentPaths[path] = struct{}{}
		}

		currentSeen := m.sessionSeen[current]
		useStored := seen > currentSeen ||
			(seen == currentSeen && id.Format(16) > current.Format(16))
		if current == id || !useStored {
			m.sessionPaths[current] = currentPaths
			if seen > currentSeen {
				m.sessionSeen[current] = seen
			}
			continue
		}

		delete(m.sessionHashes, current)
		delete(m.sessionPaths, current)
		delete(m.sessionSeen, current)
		m.sessions[key] = id
		m.sessionHashes[id] = key
		m.sessionPaths[id] = currentPaths
		m.sessionSeen[id] = seen
	}
}

func (m *ms) StoreSessions(db zdb.DB) {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()

	d, err := json.Marshal(storedSession{
		Sessions: m.sessions,
		Paths:    m.sessionPaths,
		Seen:     m.sessionSeen,
		Hashes:   m.sessionHashes,
	})
	if err != nil {
		memlog.Error(context.Background(), err)
		return
	}

	// Each process writes its own immutable snapshot. A singleton key races
	// during rolling deployments: the replacement can consume the row before
	// the old process writes it, after which its own shutdown insert conflicts.
	key := fmt.Sprintf("session:%s", UUID().Format(16))
	err = db.Exec(context.Background(),
		`insert into store (key, value) values (?, ?)`, key, d)
	if err != nil {
		memlog.Error(context.Background(), err)
	}

	memlog.Debug(context.Background(), "stored sessions in DB on shutdown",
		"bytesize", len(d),
		"sessions", len(m.sessions),
		"sessionHashes", len(m.sessionHashes),
		"sessionPaths", len(m.sessionPaths),
		"sessionSeen", len(m.sessionSeen))
}

func (m *ms) Append(hits ...Hit) {
	m.hitMu.Lock()
	m.hits = append(m.hits, hits...)
	m.hitMu.Unlock()
}

func (m *ms) SessionsLen() int {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	return len(m.sessions)
}

func (m *ms) Len() int {
	m.hitMu.Lock()
	defer m.hitMu.Unlock()
	return len(m.hits)
}

func (m *ms) Persist(ctx context.Context) ([]Hit, error) {
	if m.Len() == 0 {
		return nil, nil
	}

	m.hitMu.Lock()
	hits := make([]Hit, len(m.hits))
	copy(hits, m.hits)
	m.hits = make([]Hit, 0, 16)
	m.hitMu.Unlock()

	bot, err := zdb.NewBulkInsert(ctx, "bots", []string{"site", "path", "bot", "user_agent", "created_at"})
	if err != nil {
		return nil, err
	}
	ins, err := zdb.NewBulkInsert(ctx, "hits", []string{"site", "path_id", "ref_id", "browser_id", "system_id",
		"width", "location", "language", "created_at", "session", "first_visit", "campaign"})
	if err != nil {
		return nil, err
	}

	newHits := make([]Hit, 0, len(hits))
	for _, h := range hits {
		if h.Bot > 0 {
			bot.Values(h.Site, h.Path, h.Bot, h.UserAgentHeader, h.CreatedAt)
			continue
		}
		if m.processHit(ctx, &h) {
			// Don't return hits that failed validation; otherwise cron will try to
			// insert them.
			newHits = append(newHits, h)

			if !h.NoStore {
				var w *float64
				if len(h.Size) > 0 {
					w = &h.Size[0]
				}
				ins.Values(h.Site, h.PathID, h.RefID, h.BrowserID, h.SystemID, w, h.Location, h.Language,
					h.CreatedAt.Round(time.Second), h.Session, h.FirstVisit, h.CampaignID)
			}
		}
	}

	// Just log errors on inserting bots; not that important.
	if err := bot.Finish(); err != nil {
		memlog.Errorf(ctx, "storing bots: %s", err)
	}
	return newHits, ins.Finish()
}

func (m *ms) processHit(ctx context.Context, h *Hit) bool {
	defer log.Recover(ctx, func(err error) { memlog.Error(ctx, err, "hit", h) })

	if h.noProcess {
		return true
	}
	if h.Site == "" {
		h.Site = Config(ctx).Sites[0].Key
	}

	// Ignore spammers.
	h.RefURL, _ = url.Parse(h.Ref)
	if h.RefURL != nil {
		if refspam.Is(h.RefURL.Host) {
			refspamlog.Debugf(ctx, "refspam ignored: %q", h.RefURL.Host)
			return false
		}
	}

	site, ok := Config(ctx).Site(h.Site)
	if !ok {
		memlog.Error(ctx, "unknown site", "site", h.Site, "hit", h)
		return false
	}
	site.Defaults(ctx)
	ctx = WithSite(ctx, &site)
	var err error
	if !site.Settings.Collect.Has(CollectHits) {
		h.NoStore = true
	}

	if !site.Settings.Collect.Has(CollectReferrer) {
		h.Query, h.Ref, h.RefScheme, h.RefURL = "", "", "", nil
	}

	err = h.Defaults(ctx, false)
	if err != nil {
		if errors.As(err, new(&zvalidate.Validator{})) {
			memlog.Debug(ctx, err.Error(), "hit", h)
		} else {
			memlog.Error(ctx, err, "hit", h)
		}
		return false
	}

	if h.Session.IsZero() && site.Settings.Collect.Has(CollectSession) && !h.NoSession.Bool() {
		h.Session, h.FirstVisit = m.session(ctx, h.PathID, h.UserSessionID, h.UserAgentHeader, h.RemoteAddr)
	}

	if !site.Settings.Collect.Has(CollectSession) || h.NoSession.Bool() {
		h.Session, h.FirstVisit = zint.Uint128{}, true
	}
	if !site.Settings.Collect.Has(CollectScreenSize) {
		h.Size = nil
	}
	if !site.Settings.Collect.Has(CollectUserAgent) {
		h.UserAgentHeader, h.BrowserID, h.SystemID = "", 0, 0
	}
	if !site.Settings.Collect.Has(CollectLanguage) {
		h.Language = nil
	}
	if !site.Settings.Collect.Has(CollectLocation) {
		h.Location = ""
	}
	if strings.ContainsRune(h.Location, '-') {
		trim := !site.Settings.Collect.Has(CollectLocationRegion)
		if !trim && len(site.Settings.CollectRegions) > 0 {
			trim = !slices.Contains(site.Settings.CollectRegions, h.Location[:2])
		}
		if trim {
			var loc Location
			err := loc.ByCode(ctx, h.Location[:2])
			if err != nil {
				memlog.Errorf(ctx, "lookup %q: %s", h.Location[:2], err)
			}
			h.Location = loc.ISO3166_2
		}
	}

	if h.Ignore() {
		return false
	}

	err = h.Validate(ctx, false)
	if err != nil {
		memlog.Error(ctx, err, "hit", h)
		return false
	}
	return true
}

// SessionTime is the maximum length of sessions; exported here for tests.
var SessionTime = 8 * time.Hour

// For 10k sessions this takes about 5ms on my laptop; that's a small enough
// delay to not overly worry about (there are rarely more than a few hundred
// sessions at a time).
func (m *ms) EvictSessions(ctx context.Context) {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()

	ev := ztime.Now(ctx).Add(-SessionTime).Unix()
	for id, seen := range m.sessionSeen {
		if seen > ev {
			continue
		}

		sk := m.sessionHashes[id]

		sesslog.Debug(context.Background(), "evicting session",
			"session-id", id,
			"last-seen", seen,
			"session-key", sk)

		delete(m.sessions, sk)
		delete(m.sessionPaths, id)
		delete(m.sessionSeen, id)
		delete(m.sessionHashes, id)
	}
}

// SessionID gets a new UUID4 session ID.
func (m *ms) SessionID() zint.Uint128 {
	if m.testHook {
		TestSeqSession[1]++
		return TestSeqSession
	}
	return UUID()
}

func (m *ms) session(ctx context.Context, pathID PathID, userSessionID, ua, remoteAddr string) (zint.Uint128, zbool.Bool) {
	sk := sessionKey(MustGetSite(ctx).Key + ":" + userSessionID)
	if userSessionID == "" {
		sk = sessionKey(MustGetSite(ctx).Key + ":" + ua + "-" + remoteAddr)
	}

	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()

	id, ok := m.sessions[sk]
	if ok { // Existing session
		m.sessionSeen[id] = ztime.Now(ctx).Unix()
		_, seenPath := m.sessionPaths[id][pathID]
		if !seenPath {
			m.sessionPaths[id][pathID] = struct{}{}
		}

		sesslog.Debug(ctx, "HIT",
			"session-key", sk,
			"session-id", id,
			"path", pathID,
			"seen-path", seenPath)
		return id, zbool.Bool(!seenPath)
	}

	// New session
	id = m.SessionID()
	m.sessions[sk] = id
	m.sessionPaths[id] = map[PathID]struct{}{pathID: struct{}{}}
	m.sessionSeen[id] = ztime.Now(ctx).Unix()
	m.sessionHashes[id] = sk

	sesslog.Debug(ctx, "MISS: created new",
		"session-key", sk,
		"session-id", id,
		"path", pathID)
	return id, true
}
