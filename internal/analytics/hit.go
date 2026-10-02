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

	"github.com/marvinrabe/goatcounter/internal/datetime"
	"github.com/marvinrabe/goatcounter/internal/enrich"
)

// EventPageview is the name of pageview events.
const EventPageview = "pageview"

// Hit is one pageview or custom event, as sent by count.js and stored in the
// events table.
type Hit struct {
	// Query parameters sent by count.js.
	Path      string `json:"p,omitempty"`  // Path and query string.
	Ref       string `json:"r,omitempty"`  // Referrer URL.
	Hostname  string `json:"h,omitempty"`  // location.hostname
	Name      string `json:"n,omitempty"`  // Custom event name; empty for pageviews.
	Props     string `json:"pr,omitempty"` // Custom event properties as a JSON object.
	Width     int    `json:"s,omitempty"`  // Screen width in CSS pixels.
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
	Browser, BrowserVersion, OS, OSVersion                 string `json:"-"`
}

func (h *Hit) Ignore() bool {
	// Almost certainly some broken HTML or whatnot.
	if strings.Contains(h.Path, "<html>") || strings.Contains(h.Path, "<HTML>") {
		return true
	}
	return h.Path == "/favicon.ico"
}

// splitPath stores the path the way Plausible does: the decoded URL path
// without the query string, with a trailing slash kept as sent. The UTM
// parameters are taken from the query string.
func (h *Hit) splitPath() {
	u, err := url.Parse(strings.TrimSpace(h.Path))
	if err != nil {
		// Not a valid URL, such as a lone "%"; keep what's before the query.
		p, _, _ := strings.Cut(h.Path, "?")
		u = &url.URL{Path: p}
	}
	h.Path = strings.TrimRight(u.Path, " \t\r\n")
	if h.Path == "" {
		h.Path = "/"
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
	first(&h.UTMSource, "utm_source", "source", "ref")
	first(&h.UTMMedium, "utm_medium")
	first(&h.UTMCampaign, "utm_campaign")
	first(&h.UTMContent, "utm_content")
	first(&h.UTMTerm, "utm_term")
}

// HitFromRequest starts a hit with what the request tells: its country and
// language. The rest is sent by count.js in the query string, and derived from
// that by Defaults.
func HitFromRequest(r *http.Request) Hit {
	return Hit{
		CreatedAt:       datetime.Now(),
		UserAgentHeader: r.UserAgent(),
		RemoteAddr:      r.RemoteAddr,
		Country:         enrich.Country(r),
		Language:        enrich.Language(r.Header.Get("Accept-Language")),
	}
}

// Defaults normalizes the hit for a site and derives the stored dimensions.
func (h *Hit) Defaults(site Site) {
	h.Site = site.Key
	if h.CreatedAt.IsZero() {
		h.CreatedAt = datetime.Now()
	}
	h.Name = strings.TrimSpace(h.Name)
	if h.Name == "" {
		h.Name = EventPageview
	}
	h.Hostname = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(h.Hostname)), "www.")
	h.splitPath()
	h.Source, h.Referrer = enrich.Source(h.Ref, site.LinkDomain)
	if h.UTMSource != "" {
		h.Source = h.UTMSource
	}
	ua := enrich.ParseUserAgent(h.UserAgentHeader)
	h.Browser, h.BrowserVersion, h.OS, h.OSVersion = ua.Browser, ua.BrowserVersion, ua.OS, ua.OSVersion
}

// Validate the request before it's normalized. Props are normalized to a
// compact JSON object with string values.
func (h *Hit) Validate() error {
	var errs []string
	check := func(ok bool, field, msg string) {
		if !ok {
			errs = append(errs, field+": "+msg)
		}
	}
	check(h.Path != "", "path", "must be set")
	for _, f := range []struct {
		name, value string
		max         int
	}{
		{"path", h.Path, 2048},
		{"ref", h.Ref, 2048},
		{"name", h.Name, 120},
		{"hostname", h.Hostname, 253},
		{"user_agent_header", h.UserAgentHeader, 512},
	} {
		check(utf8.ValidString(f.value), f.name, "must be UTF-8")
		check(utf8.RuneCountInString(f.value) <= f.max, f.name, fmt.Sprintf("must be at most %d characters", f.max))
	}
	check(h.Width >= 0 && h.Width <= 100_000, "width", "must be between 0 and 100000")
	if h.Props != "" {
		var props map[string]string
		switch err := json.Unmarshal([]byte(h.Props), &props); {
		case err != nil:
			check(false, "props", "must be a JSON object with string values")
		case len(props) > 30:
			check(false, "props", "at most 30 properties")
		case len(props) == 0:
			h.Props = ""
		default:
			b, _ := json.Marshal(props)
			check(len(b) <= 4096, "props", "longer than 4096 bytes")
			h.Props = string(b)
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}
