package analytics

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/marvinrabe/goatcounter/internal/enrich"
)

// EventPageview is the name of pageview events.
const EventPageview = "pageview"

// Event is one pageview or custom event, as sent by count.js and stored in the
// events table.
type Event struct {
	// Query parameters sent by count.js.
	Path      string `json:"p,omitempty"`  // Path and query string.
	Ref       string `json:"r,omitempty"`  // Referrer URL.
	Hostname  string `json:"h,omitempty"`  // location.hostname
	Name      string `json:"n,omitempty"`  // Custom event name; empty for pageviews.
	Props     string `json:"pr,omitempty"` // Custom event properties as a JSON object.
	Bot       int    `json:"b,omitempty"`
	NoSession bool   `json:"ns,omitempty"`

	// Set from the request.
	Site            string    `json:"-"`
	CreatedAt       time.Time `json:"-"`
	UserAgentHeader string    `json:"-"`
	RemoteAddr      string    `json:"-"`
	Language        string    `json:"-"`
	Country         string    `json:"-"` // ISO 3166-1 alpha-2.

	// Derived by Defaults().
	Source, Referrer                                       string `json:"-"`
	UTMSource, UTMMedium, UTMCampaign, UTMContent, UTMTerm string `json:"-"`
	Browser, BrowserVersion, OS, Device                    string `json:"-"`
}

func (e *Event) Ignore() bool {
	// Almost certainly some broken HTML or whatnot.
	if strings.Contains(e.Path, "<html>") || strings.Contains(e.Path, "<HTML>") {
		return true
	}
	return e.Path == "/favicon.ico"
}

// splitPath stores the path the way Plausible does: the decoded URL path
// without the query string, with a trailing slash kept as sent. The UTM
// parameters are taken from the query string.
func (e *Event) splitPath() {
	u, err := url.Parse(strings.TrimSpace(e.Path))
	if err != nil {
		// Not a valid URL, such as a lone "%"; keep what's before the query.
		p, _, _ := strings.Cut(e.Path, "?")
		u = &url.URL{Path: p}
	}
	e.Path = strings.TrimRight(u.Path, " \t\r\n")
	if e.Path == "" {
		e.Path = "/"
	}

	// The first non-empty value wins, as in Plausible.
	q := u.Query()
	first := func(dst *string, keys ...string) {
		for _, k := range keys {
			if v := strings.TrimSpace(q.Get(k)); v != "" && *dst == "" {
				*dst = v
			}
		}
	}
	first(&e.UTMSource, "utm_source", "source", "ref")
	first(&e.UTMMedium, "utm_medium")
	first(&e.UTMCampaign, "utm_campaign")
	first(&e.UTMContent, "utm_content")
	first(&e.UTMTerm, "utm_term")
}

// EventFromRequest starts an event with what the request tells: its country and
// language. The rest is sent by count.js in the query string, and derived from
// that by Defaults.
func EventFromRequest(r *http.Request) Event {
	return Event{
		CreatedAt:       time.Now().UTC(),
		UserAgentHeader: r.UserAgent(),
		RemoteAddr:      r.RemoteAddr,
		Country:         enrich.Country(r),
		Language:        enrich.Language(r.Header.Get("Accept-Language")),
	}
}

// Defaults normalizes the event for a site and derives the stored dimensions.
func (e *Event) Defaults(site Site) {
	e.Site = site.Key
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	e.Name = strings.TrimSpace(e.Name)
	if e.Name == "" {
		e.Name = EventPageview
	}
	e.Hostname = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(e.Hostname)), "www.")
	e.splitPath()
	e.Source, e.Referrer = enrich.Source(e.Ref, site.LinkDomain)
	if e.UTMSource != "" {
		e.Source = e.UTMSource
	}
	ua := enrich.ParseUserAgent(e.UserAgentHeader)
	e.Browser, e.BrowserVersion, e.OS, e.Device = ua.Browser, ua.BrowserVersion, ua.OS, ua.Device
}

// Validate the request before it's normalized. Props are normalized to a
// compact JSON object with string values.
func (e *Event) Validate() error {
	var errs []string
	check := func(ok bool, field, msg string) {
		if !ok {
			errs = append(errs, field+": "+msg)
		}
	}
	check(e.Path != "", "path", "must be set")
	for _, f := range []struct {
		name, value string
		max         int
	}{
		{"path", e.Path, 2048},
		{"ref", e.Ref, 2048},
		{"name", e.Name, 120},
		{"hostname", e.Hostname, 253},
		{"user_agent_header", e.UserAgentHeader, 512},
	} {
		check(utf8.ValidString(f.value), f.name, "must be UTF-8")
		check(utf8.RuneCountInString(f.value) <= f.max, f.name, fmt.Sprintf("must be at most %d characters", f.max))
	}
	if e.Props != "" {
		var props map[string]string
		switch err := json.Unmarshal([]byte(e.Props), &props); {
		case err != nil:
			check(false, "props", "must be a JSON object with string values")
		case len(props) > 30:
			check(false, "props", "at most 30 properties")
		case len(props) == 0:
			e.Props = ""
		default:
			b, _ := json.Marshal(props)
			check(len(b) <= 4096, "props", "longer than 4096 bytes")
			e.Props = string(b)
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}
