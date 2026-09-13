package goatcounter

import (
	"context"
	"fmt"
	"strings"

	botcheck "github.com/marvinrabe/goatcounter/internal/bot"
	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/mileusna/useragent"
)

type UserAgent struct {
	UserAgent string
	Isbot     uint8
	BrowserID BrowserID
	SystemID  SystemID
}

func (p *UserAgent) GetOrInsert(ctx context.Context) error {
	key := p.UserAgent
	cache := batchCacheFor(ctx).agents
	c, ok := cache[key]
	if ok {
		*p = c
		return nil
	}

	var (
		ua      = parseUserAgent(p.UserAgent)
		browser Browser
		system  System
	)

	err := browser.GetOrInsert(ctx, ua.Name, ua.Version)
	if err != nil {
		return fmt.Errorf("UserAgent.GetOrInsert: %w", err)
	}
	p.BrowserID = browser.ID

	err = system.GetOrInsert(ctx, ua.OS, ua.OSVersion)
	if err != nil {
		return fmt.Errorf("UserAgent.GetOrInsert: %w", err)
	}
	p.SystemID = system.ID

	p.Isbot = uint8(botcheck.UserAgent(p.UserAgent))

	if cache != nil {
		cache[key] = *p
	}
	return nil
}

type BrowserID int32

type Browser struct {
	ID      BrowserID `db:"browser_id,id" json:"id"`
	Name    string    `db:"name" json:"name"`
	Version string    `db:"version" json:"version"`
}

func (Browser) Table() string { return "browsers" }

func (b *Browser) GetOrInsert(ctx context.Context, name, version string) error {
	k := name + version
	cache := batchCacheFor(ctx).browsers
	c, ok := cache[k]
	if ok {
		*b = c
		return nil
	}

	b.Name, b.Version = name, version

	err := database.Get(ctx, &b.ID, `select browser_id from browsers where name=? and version=?`, name, version)
	if database.ErrNoRows(err) {
		err = database.Insert(ctx, b)
	}
	if err != nil {
		return fmt.Errorf("Browser.GetOrInsert(%q, %q): %w", name, version, err)
	}
	if cache != nil {
		cache[k] = *b
	}
	return nil
}

type SystemID int32

type System struct {
	ID      SystemID `db:"system_id,id" json:"id"`
	Name    string   `db:"name" json:"name"`
	Version string   `db:"version" json:"version"`
}

func (System) Table() string { return "systems" }

func (s *System) GetOrInsert(ctx context.Context, name, version string) error {
	k := name + version
	cache := batchCacheFor(ctx).systems
	c, ok := cache[k]
	if ok {
		*s = c
		return nil
	}

	s.Name, s.Version = name, version

	err := database.Get(ctx, &s.ID, `select system_id from systems where name=? and version=?`, name, version)
	if database.ErrNoRows(err) {
		err = database.Insert(ctx, s)
	}
	if err != nil {
		return fmt.Errorf("System.GetOrInsert(%q, %q): %w", name, version, err)
	}
	if cache != nil {
		cache[k] = *s
	}
	return nil
}

// Preserve the dashboard's version grouping while using the maintained parser.
func parseUserAgent(raw string) useragent.UserAgent {
	ua := useragent.Parse(raw)
	version := func(s string, n int) string {
		parts := strings.Split(s, ".")
		return strings.Join(parts[:min(n, len(parts))], ".")
	}
	switch ua.Name {
	case "Chrome", "Chromium", "Headless Chrome", "Firefox", "Edge":
		ua.Version = version(ua.Version, 1)
	default:
		ua.Version = version(ua.Version, 2)
	}
	switch ua.OS {
	case "Linux":
		ua.OSVersion = ""
		for _, distro := range []string{"Ubuntu", "CentOS", "Fedora", "Debian"} {
			if strings.Contains(raw, distro) {
				ua.OSVersion = distro
				break
			}
		}
	case "Windows", "Android", "Windows Phone":
		ua.OSVersion = strings.TrimSuffix(version(ua.OSVersion, 2), ".0")
	case "ChromeOS":
		ua.OS = "Chrome OS"
		ua.OSVersion = ""
	default:
		ua.OSVersion = version(ua.OSVersion, 2)
	}
	return ua
}
