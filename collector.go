package goatcounter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/gob"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/datetime"
	"github.com/marvinrabe/goatcounter/internal/refspam"
	"github.com/marvinrabe/goatcounter/internal/validation"
)

// TestSession is a fixed session identifier used by data fixtures.
var TestSession = uuid.MustParse("00112233-4455-6677-8899-aabbccddeeff")

// EnqueueHits durably accepts work before the collector returns success. There
// is no process-local queue: any replica can process any committed entry.
func EnqueueHits(ctx context.Context, hits ...Hit) error {
	return retryBusy(ctx, func() error {
		return database.TX(ctx, func(ctx context.Context) error {
			ins, err := database.NewBulkInsert(ctx, "hit_queue", []string{"site", "payload"})
			if err != nil {
				return err
			}
			for _, h := range hits {
				if h.Site == "" {
					h.Site = Config(ctx).Sites[0].Key
				}
				if h.CreatedAt.IsZero() {
					h.CreatedAt = datetime.Now(ctx)
				}
				// Never put a raw client IP address in the durable inbox.
				identity := h.UserSessionID
				if identity == "" {
					identity = h.UserAgentHeader + "\x00" + h.RemoteAddr
				}
				key := sha256.Sum256([]byte(h.Site + "\x00" + identity))
				h.UserSessionID = fmt.Sprintf("%x", key)
				h.RemoteAddr = ""
				var payload bytes.Buffer
				if err := gob.NewEncoder(&payload).Encode(h); err != nil {
					return err
				}
				ins.Values(h.Site, payload.Bytes())
			}
			return ins.Finish()
		})
	})
}

const HitBatchSize = 200

// PersistHits processes one bounded batch. Claiming work, session decisions,
// raw hits, aggregates, and acknowledgement all commit together. An error or
// killed worker rolls back the entire batch so another replica can retry it.
func PersistHits(ctx context.Context, updateStats func(context.Context, []Hit) error) ([]Hit, error) {
	var hits []Hit
	err := retryBusy(ctx, func() error {
		hits = nil
		return database.TX(ctx, func(ctx context.Context) error {
			// A transaction must not publish uncommitted dimension IDs, or
			// reuse IDs cached before a different replica deleted a path.
			ctx = NewBatchCache(ctx)
			sites := make([]string, 0, len(Config(ctx).Sites))
			for _, site := range Config(ctx).Sites {
				sites = append(sites, site.Key)
			}
			var rows []struct {
				ID      int64  `db:"id"`
				Payload []byte `db:"payload"`
			}
			// The first statement takes the database write lock. DELETE's
			// changes stay uncommitted until every derived record is saved.
			err := database.Select(ctx, &rows, `delete from hit_queue where id in (
                select id from hit_queue where site in (:sites) order by id limit :limit
            ) returning id, payload`, map[string]any{"sites": sites, "limit": HitBatchSize})
			if err != nil {
				return err
			}
			slices.SortFunc(rows, func(a, b struct {
				ID      int64  `db:"id"`
				Payload []byte `db:"payload"`
			}) int {
				if a.ID < b.ID {
					return -1
				}
				if a.ID > b.ID {
					return 1
				}
				return 0
			})
			ins, err := database.NewBulkInsert(ctx, "hits", []string{"site", "path_id", "ref_id", "browser_id", "system_id",
				"width", "location", "language", "created_at", "session", "first_visit", "campaign"})
			if err != nil {
				return err
			}
			for _, row := range rows {
				var h Hit
				if err := gob.NewDecoder(bytes.NewReader(row.Payload)).Decode(&h); err != nil {
					return err
				}
				if h.Bot > 0 {
					if err := database.Exec(ctx, `insert into bots(site,path,bot,user_agent,created_at) values(?,?,?,?,?)`,
						h.Site, h.Path, h.Bot, h.UserAgentHeader, h.CreatedAt); err != nil {
						return err
					}
					continue
				}
				ok, err := processHit(ctx, &h)
				if err != nil {
					return err
				}
				if !ok {
					continue
				}
				hits = append(hits, h)
				var w *float64
				if len(h.Size) > 0 {
					w = &h.Size[0]
				}
				ins.Values(h.Site, h.PathID, h.RefID, h.BrowserID, h.SystemID, w, h.Location, h.Language,
					h.CreatedAt.Round(time.Second), h.Session, h.FirstVisit, h.CampaignID)
			}
			if err := ins.Finish(); err != nil {
				return err
			}
			if updateStats != nil {
				return updateStats(ctx, hits)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

// Retry only explicit lock contention, which has a known uncommitted outcome.
// Connection failures and ambiguous commits are never blindly replayed.
func retryBusy(ctx context.Context, run func() error) error {
	for delay := 10 * time.Millisecond; ; delay = min(delay*2, 100*time.Millisecond) {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := run()
		if err == nil || (!strings.Contains(err.Error(), "database is locked") &&
			!strings.Contains(err.Error(), "SQLITE_BUSY") && !strings.Contains(err.Error(), "SQLITE_LOCKED")) {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func processHit(ctx context.Context, h *Hit) (bool, error) {

	if h.Site == "" {
		h.Site = Config(ctx).Sites[0].Key
	}

	// Ignore spammers.
	h.RefURL, _ = url.Parse(h.Ref)
	if h.RefURL != nil {
		if refspam.Is(h.RefURL.Host) {
			slog.With("module", "refspam").DebugContext(ctx, "refspam ignored", "host", h.RefURL.Host)
			return false, nil
		}
	}

	site, ok := Config(ctx).Site(h.Site)
	if !ok {
		slog.With("module", "collector").ErrorContext(ctx, "unknown site", "site", h.Site, "hit", h)
		return false, nil
	}
	site.Defaults()
	ctx = WithSite(ctx, &site)
	err := h.Defaults(ctx, false)
	if err != nil {
		if errors.As(err, new(&validation.Validator{})) {
			slog.With("module", "collector").DebugContext(ctx, "discard invalid hit",
				"error", err, "hit", h)
		} else {
			return false, err
		}
		return false, nil
	}

	if h.Session == uuid.Nil() && !h.NoSession {
		h.Session, h.FirstVisit, err = session(ctx, h.PathID, h.UserSessionID)
		if err != nil {
			return false, err
		}
	}

	if h.NoSession {
		h.Session, h.FirstVisit = SessionID{}, true
	}

	if h.Ignore() {
		return false, nil
	}

	err = h.Validate(ctx, false)
	if err != nil {
		slog.With("module", "collector").ErrorContext(ctx, "discard invalid normalized hit",
			"error", err, "hit", h)
		return false, nil
	}
	return true, nil
}

// SessionTime is the idle lifetime of a visitor session.
var SessionTime = 8 * time.Hour

// session runs inside the batch's write transaction, serializing the lookup
// and first-path decision across all collectors sharing the database.
func session(ctx context.Context, pathID PathID, key string) (SessionID, bool, error) {
	var row struct {
		ID   []byte `db:"session"`
		Seen int64  `db:"seen_at"`
	}
	var id SessionID
	now := datetime.Now(ctx).Unix()
	err := database.Get(ctx, &row, `select session, seen_at from collector_sessions where key=?`, key)
	if err != nil && !database.ErrNoRows(err) {
		return id, false, err
	}
	copy(id[:], row.ID)
	if database.ErrNoRows(err) || row.Seen < now-int64(SessionTime.Seconds()) {
		if id != uuid.Nil() {
			if err := database.Exec(ctx, `delete from collector_session_paths where session=?`, id); err != nil {
				return id, false, err
			}
		}
		id = uuid.NewV4()
	}
	if err := database.Exec(ctx, `insert into collector_sessions(key,site,session,seen_at) values(?,?,?,?)
		on conflict(key) do update set session=excluded.session, seen_at=excluded.seen_at`, key, MustGetSite(ctx).Key, id, now); err != nil {
		return id, false, err
	}
	n, err := database.NumRows(ctx, `insert into collector_session_paths(session,path_id) values(?,?)
		on conflict(session,path_id) do nothing`, id, pathID)
	return id, n == 1, err
}

func EvictSessions(ctx context.Context) error {
	return database.TX(ctx, func(ctx context.Context) error {
		var ids [][]byte
		if err := database.Select(ctx, &ids, `delete from collector_sessions where seen_at < ? returning session`,
			datetime.Now(ctx).Add(-SessionTime).Unix()); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		return database.Exec(ctx, `delete from collector_session_paths where session in (:ids)`, map[string]any{"ids": ids})
	})
}
