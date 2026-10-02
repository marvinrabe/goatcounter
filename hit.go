package goatcounter

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/marvinrabe/goatcounter/internal/datetime"
	"github.com/marvinrabe/goatcounter/internal/validation"
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
	Site            string      `json:"-"`
	CreatedAt       time.Time   `json:"-"`
	UserAgentHeader string      `json:"-"`
	RemoteAddr      string      `json:"-"`
	Language        string      `json:"-"`
	Location        GeoLocation `json:"-"`

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

// Defaults normalizes the hit and derives the stored dimensions. It never
// touches the database.
func (h *Hit) Defaults(ctx context.Context) {
	if h.Site == "" {
		h.Site = MustGetSite(ctx).Key
	}
	if h.CreatedAt.IsZero() {
		h.CreatedAt = datetime.Now(ctx)
	}
	h.Name = strings.TrimSpace(h.Name)
	if h.Name == "" {
		h.Name = EventPageview
	}
	h.Hostname = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(h.Hostname)), "www.")
	h.splitPath()
	h.Source, h.Referrer = referrerSource(h.Ref, MustGetSite(ctx).LinkDomain)
	if h.UTMSource != "" {
		h.Source = h.UTMSource
	}

	ua := parseUserAgent(h.UserAgentHeader)
	h.Browser, h.BrowserVersion, h.OS, h.OSVersion = ua.Name, ua.Version, ua.OS, ua.OSVersion
}

// Validate the request before it's normalized. Props are normalized to a
// compact JSON object with string values.
func (h *Hit) Validate(ctx context.Context) error {
	v := validation.New()
	v.Required("path", h.Path)
	v.UTF8("path", h.Path)
	v.Len("path", h.Path, 1, 2048)
	v.UTF8("ref", h.Ref)
	v.Len("ref", h.Ref, 0, 2048)
	v.UTF8("name", h.Name)
	v.Len("name", h.Name, 0, 120)
	v.Len("hostname", h.Hostname, 0, 253)
	v.UTF8("user_agent_header", h.UserAgentHeader)
	v.Len("user_agent_header", h.UserAgentHeader, 0, 512)
	v.Range("width", int64(h.Width), 0, 100_000)
	if h.Props != "" {
		var props map[string]string
		switch err := json.Unmarshal([]byte(h.Props), &props); {
		case err != nil:
			v.Append("props", "must be a JSON object with string values")
		case len(props) > 30:
			v.Append("props", "at most 30 properties")
		case len(props) == 0:
			h.Props = ""
		default:
			b, _ := json.Marshal(props)
			if len(b) > 4096 {
				v.Append("props", "longer than 4096 bytes")
			}
			h.Props = string(b)
		}
	}
	return v.ErrorOrNil()
}
