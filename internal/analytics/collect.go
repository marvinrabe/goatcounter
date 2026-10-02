package analytics

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"time"
)

// SessionTimeout is the inactivity after which a visitor starts a new visit;
// the same as Plausible.
const SessionTimeout = 30 * time.Minute

// Collect stores one event with a single INSERT statement.
//
// There is no queue and no background processing: the session is resolved
// in the same statement from the visitor's most recent event in the last 30
// minutes, so any replica in any region can accept any event, and nothing
// runs while there is no traffic.
func (s *Store) Collect(ctx context.Context, site Site, e Event) error {
	e.Defaults(site)
	if e.Ignore() {
		return nil
	}

	salt, prevSalt, err := s.daySalts(ctx, e.CreatedAt)
	if err != nil {
		return fmt.Errorf("Collect: %w", err)
	}
	visitor, prevVisitor := visitorID(salt, &e), visitorID(prevSalt, &e)
	if prevVisitor == 0 {
		prevVisitor = -1 // No salt for yesterday; visitor IDs are never negative.
	}
	if e.NoSession {
		visitor, prevVisitor = rand.Int64(), -1
	}

	err = s.DB.Exec(ctx, `insert into events (
			site, ts, visitor, session, name, hostname, path, props,
			source, referrer, utm_source, utm_medium, utm_campaign, utm_content, utm_term,
			browser, browser_version, os, os_version, device,
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
			:browser, :browser_version, :os, :os_version, :device,
			:country, :language
		)`, named(map[string]any{
		"site": e.Site, "ts": e.CreatedAt.Unix(),
		"visitor": visitor, "prev_visitor": prevVisitor,
		"since":   e.CreatedAt.Add(-SessionTimeout).Unix(),
		"session": rand.Int64(),
		"name":    e.Name, "hostname": e.Hostname, "path": e.Path, "props": e.Props,
		"source": e.Source, "referrer": e.Referrer,
		"utm_source": e.UTMSource, "utm_medium": e.UTMMedium, "utm_campaign": e.UTMCampaign,
		"utm_content": e.UTMContent, "utm_term": e.UTMTerm,
		"browser": e.Browser, "browser_version": e.BrowserVersion,
		"os": e.OS, "os_version": e.OSVersion, "device": e.Device,
		"country":  e.Country,
		"language": e.Language,
	})...)
	if err != nil {
		return fmt.Errorf("Collect: %w", err)
	}
	return nil
}

// visitorID is a 63-bit hash of the salt, site, IP address, and User-Agent.
// The IP address itself is never stored.
func visitorID(salt []byte, e *Event) int64 {
	if len(salt) == 0 {
		return 0
	}
	sum := sha256.New()
	sum.Write(salt)
	for _, s := range []string{e.Site, e.RemoteAddr, e.UserAgentHeader} {
		sum.Write([]byte(s))
		sum.Write([]byte{0})
	}
	return int64(binary.BigEndian.Uint64(sum.Sum(nil)) >> 1)
}

// daySalts gets the salts for today and yesterday. Salts rotate at midnight
// in the dashboard timezone, so visitors are unique per dashboard day.
// Yesterday's salt is kept so that a visit which crosses midnight remains one
// visit; older salts are deleted.
func (s *Store) daySalts(ctx context.Context, now time.Time) (cur, prev []byte, err error) {
	local := now.In(s.Timezone.Loc())
	day := local.Format("2006-01-02")
	s.salts.mu.Lock()
	defer s.salts.mu.Unlock()
	if s.salts.day == day {
		return s.salts.cur, s.salts.prev, nil
	}

	yesterday := local.AddDate(0, 0, -1).Format("2006-01-02")
	newSalt := make([]byte, 16)
	if _, err := cryptorand.Read(newSalt); err != nil {
		return nil, nil, err
	}
	// Every replica races to insert the salt for a new day; the first one
	// wins and the others read it back.
	if err := s.DB.Exec(ctx, `insert into salts (day, salt) values (?, ?)
		on conflict (day) do nothing`, day, newSalt); err != nil {
		return nil, nil, err
	}
	if err := s.DB.Exec(ctx, `delete from salts where day < ?`, yesterday); err != nil {
		return nil, nil, err
	}
	var rows []struct {
		Day  string `db:"day"`
		Salt []byte `db:"salt"`
	}
	if err := s.DB.Select(ctx, &rows, `select day, salt from salts where day in (?, ?)`, day, yesterday); err != nil {
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
	s.salts.day, s.salts.cur, s.salts.prev = day, cur, prev
	return cur, prev, nil
}
