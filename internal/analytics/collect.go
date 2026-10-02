package analytics

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/marvinrabe/goatcounter/internal/database"
)

// SessionTimeout is the inactivity after which a visitor starts a new visit;
// the same as Plausible.
const SessionTimeout = 30 * time.Minute

// Collect stores one hit with a single INSERT statement.
//
// There is no queue and no background processing: the session is resolved
// in the same statement from the visitor's most recent hit in the last 30
// minutes, so any replica in any region can accept any hit, and nothing
// runs while there is no traffic.
func Collect(ctx context.Context, h Hit) error {
	h.Defaults(ctx)
	if h.Ignore() {
		return nil
	}

	salt, prevSalt, err := salts(ctx, h.CreatedAt)
	if err != nil {
		return fmt.Errorf("Collect: %w", err)
	}
	visitor, prevVisitor := visitorID(salt, &h), visitorID(prevSalt, &h)
	if prevVisitor == 0 {
		prevVisitor = -1 // No salt for yesterday; visitor IDs are never negative.
	}
	if h.NoSession {
		visitor, prevVisitor = rand.Int64(), -1
	}

	err = database.Exec(ctx, `insert into events (
			site, ts, visitor, session, name, hostname, path, props,
			source, referrer, utm_source, utm_medium, utm_campaign, utm_content, utm_term,
			browser, browser_version, os, os_version, width,
			country, language
		) values (
			:site, :ts, :visitor,
			coalesce((
				select session from events
				where site = :site and aggregate = '' and ts >= :since and visitor in (:visitor, :prev_visitor)
				order by ts desc limit 1
			), :session),
			:name, :hostname, :path, :props,
			:source, :referrer, :utm_source, :utm_medium, :utm_campaign, :utm_content, :utm_term,
			:browser, :browser_version, :os, :os_version, :width,
			:country, :language
		)`, map[string]any{
		"site": h.Site, "ts": h.CreatedAt.Unix(),
		"visitor": visitor, "prev_visitor": prevVisitor,
		"since":   h.CreatedAt.Add(-SessionTimeout).Unix(),
		"session": rand.Int64(),
		"name":    h.Name, "hostname": h.Hostname, "path": h.Path, "props": h.Props,
		"source": h.Source, "referrer": h.Referrer,
		"utm_source": h.UTMSource, "utm_medium": h.UTMMedium, "utm_campaign": h.UTMCampaign,
		"utm_content": h.UTMContent, "utm_term": h.UTMTerm,
		"browser": h.Browser, "browser_version": h.BrowserVersion,
		"os": h.OS, "os_version": h.OSVersion, "width": h.Width,
		"country":  h.Country,
		"language": h.Language,
	})
	if err != nil {
		return fmt.Errorf("Collect: %w", err)
	}
	return nil
}

// visitorID is a 63-bit hash of the salt, site, IP address, and User-Agent.
// The IP address itself is never stored.
func visitorID(salt []byte, h *Hit) int64 {
	if len(salt) == 0 {
		return 0
	}
	sum := sha256.New()
	sum.Write(salt)
	for _, s := range []string{h.Site, h.RemoteAddr, h.UserAgentHeader} {
		sum.Write([]byte(s))
		sum.Write([]byte{0})
	}
	return int64(binary.BigEndian.Uint64(sum.Sum(nil)) >> 1)
}

// Salts rotate at midnight in the dashboard timezone, so visitors are unique
// per dashboard day. Yesterday's salt is kept so that a visit which
// crosses midnight remains one visit; older salts are deleted.
var saltCache struct {
	mu        sync.Mutex
	db        database.DB
	day       string
	cur, prev []byte
}

func salts(ctx context.Context, now time.Time) (cur, prev []byte, err error) {
	local := now.In(Config(ctx).Timezone.Loc())
	day := local.Format("2006-01-02")
	saltCache.mu.Lock()
	defer saltCache.mu.Unlock()
	db := database.MustGetDB(ctx)
	if saltCache.day == day && saltCache.db == db {
		return saltCache.cur, saltCache.prev, nil
	}

	yesterday := local.AddDate(0, 0, -1).Format("2006-01-02")
	newSalt := make([]byte, 16)
	if _, err := cryptorand.Read(newSalt); err != nil {
		return nil, nil, err
	}
	// Every replica races to insert the salt for a new day; the first one
	// wins and the others read it back.
	if err := database.Exec(ctx, `insert into salts (day, salt) values (?, ?)
		on conflict (day) do nothing`, day, newSalt); err != nil {
		return nil, nil, err
	}
	if err := database.Exec(ctx, `delete from salts where day < ?`, yesterday); err != nil {
		return nil, nil, err
	}
	var rows []struct {
		Day  string `db:"day"`
		Salt []byte `db:"salt"`
	}
	if err := database.Select(ctx, &rows, `select day, salt from salts where day in (?, ?)`, day, yesterday); err != nil {
		return nil, nil, err
	}
	for _, r := range rows {
		if r.Day == day {
			cur = r.Salt
		} else {
			prev = r.Salt
		}
	}
	if len(cur) == 0 {
		return nil, nil, fmt.Errorf("salt for %s not found", day)
	}
	saltCache.db, saltCache.day, saltCache.cur, saltCache.prev = db, day, cur, prev
	return cur, prev, nil
}
