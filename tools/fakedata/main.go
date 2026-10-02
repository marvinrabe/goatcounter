// Command fakedata stores generated traffic through the collector, for
// trying the dashboard locally. It's a development tool, not part of the
// server.
//
//	TZ=Europe/Berlin go run ./tools/fakedata -db goatcounter.db -site example.com -from 2026-09-14 -to 2026-10-02
//
// Paths, sources, and locations are picked with the same distribution as the
// site's migrated Plausible rows, if there are any. It prints a JSON summary
// of what it stored, per local day, to check the dashboard against.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"time"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/datetime"
	libsqldriver "github.com/marvinrabe/goatcounter/internal/dbdriver/libsql"
)

// Day is what was stored for one day; visitors are unique per day.
type Day struct {
	Visits, Pageviews, Bounces, Duration int
	Visitors                             int
	Browsers, Countries, Sources         map[string]int // Visits.
	Pages                                map[string]int // Unique visitors.
	Events                               map[string]int // Unique visitors.
}

type weighted struct {
	Value  string `db:"value"`
	Value2 string `db:"value2"`
	Weight int    `db:"weight"`
}

func pick(r *rand.Rand, w []weighted) weighted {
	total := 0
	for _, x := range w {
		total += x.Weight
	}
	n := r.IntN(total)
	for _, x := range w {
		if n -= x.Weight; n < 0 {
			return x
		}
	}
	return w[len(w)-1]
}

var agents = []struct {
	browser, ua string
	width       float64
	weight      int
}{
	{"Chrome", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36", 1920, 30},
	{"Microsoft Edge", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36 Edg/129.0.2792.65", 1536, 15},
	{"Safari", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15", 1440, 12},
	{"Firefox", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:131.0) Gecko/20100101 Firefox/131.0", 1920, 10},
	{"Safari", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1", 390, 18},
	{"Chrome", "Mozilla/5.0 (Linux; Android 14; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Mobile Safari/537.36", 412, 10},
	{"Safari", "Mozilla/5.0 (iPad; CPU OS 17_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.6 Mobile/15E148 Safari/604.1", 820, 5},
}

func main() {
	var (
		dbFlag   = flag.String("db", os.Getenv("GOATCOUNTER_DB"), "Database.")
		siteFlag = flag.String("site", "", "Site name.")
		fromFlag = flag.String("from", "", "First day, YYYY-MM-DD.")
		toFlag   = flag.String("to", "", "Last day, YYYY-MM-DD.")
		perDay   = flag.Int("visits", 6, "Average visits per day.")
		seed     = flag.Uint64("seed", 1, "Random seed.")
	)
	flag.Parse()
	if err := run(*dbFlag, *siteFlag, *fromFlag, *toFlag, *perDay, *seed); err != nil {
		fmt.Fprintln(os.Stderr, "fakedata:", err)
		os.Exit(1)
	}
}

func run(connect, siteName, from, to string, perDay int, seed uint64) error {
	tz, err := datetime.LoadTimezone()
	if err != nil {
		return err
	}
	loc := tz.Loc()
	first, err := time.ParseInLocation("2006-01-02", from, loc)
	if err != nil {
		return err
	}
	last, err := time.ParseInLocation("2006-01-02", to, loc)
	if err != nil {
		return err
	}

	ctx := context.Background()
	db, err := libsqldriver.Open(ctx, database.ConnectOptions{Connect: connect})
	if err != nil {
		return err
	}
	defer db.Close()
	ctx = goatcounter.NewContext(ctx, db)
	site := goatcounter.Site{Key: siteName, LinkDomain: siteName}
	goatcounter.Config(ctx).Sites = []goatcounter.Site{site}
	goatcounter.Config(ctx).Timezone = tz
	ctx = goatcounter.WithSite(ctx, &site)

	dist := func(aggregate, value, value2, metric string, fallback []weighted) ([]weighted, error) {
		var w []weighted
		err := database.Select(ctx, &w, `select `+value+` as value, `+value2+` as value2, sum(`+metric+`) as weight
			from events where site = ? and aggregate = ? and `+metric+` > 0 group by 1, 2`, siteName, aggregate)
		if len(w) == 0 {
			w = fallback
		}
		return w, err
	}
	fallbackPages := []weighted{{"/", "", 5}, {"/about/", "", 2}, {"/blog/", "", 3}}
	pages, err := dist("entry_pages", "path", "''", "entrances", fallbackPages)
	if err != nil {
		return err
	}
	allPages, err := dist("pages", "path", "''", "visitors", fallbackPages)
	if err != nil {
		return err
	}
	sources, err := dist("sources", "referrer", "utm_campaign", "visits", []weighted{{"", "", 5}, {"google.com", "", 4}})
	if err != nil {
		return err
	}
	countries, err := dist("locations", "country", "region", "visits", []weighted{{"DE", "DE-HE", 5}, {"US", "", 1}})
	if err != nil {
		return err
	}
	var agentW []weighted
	for i, a := range agents {
		agentW = append(agentW, weighted{Value: fmt.Sprint(i), Weight: a.weight})
	}

	r := rand.New(rand.NewPCG(seed, seed))
	days := make(map[string]*Day)
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		day := &Day{Browsers: map[string]int{}, Countries: map[string]int{}, Sources: map[string]int{},
			Pages: map[string]int{}, Events: map[string]int{}}
		days[d.Format("2006-01-02")] = day

		visitors := 1 + r.IntN(2*perDay)
		for v := range visitors {
			agent := agents[mustAtoi(pick(r, agentW).Value)]
			ip := fmt.Sprintf("10.%d.%d.%d", r.IntN(250), v, 1+r.IntN(250))
			loc := pick(r, countries)
			seenPage := make(map[string]bool)
			hadEvent := false
			day.Visitors++

			// Some visitors come back later the same day: a new visit.
			nVisits := 1
			if r.IntN(6) == 0 {
				nVisits = 2
			}
			// Spread start times over the day, early enough to fit all visits.
			at := d.Add(time.Duration(r.IntN(14*60)) * time.Minute).Add(time.Duration(r.IntN(60)) * time.Second)
			for range nVisits {
				src := pick(r, sources)
				ref := ""
				if src.Value != "" {
					ref = "https://www." + src.Value + "/"
				}
				path := pick(r, pages).Value
				entry := path
				if src.Value2 != "" {
					entry += "?utm_source=newsletter&utm_medium=email&utm_campaign=" + urlEscape(src.Value2)
				}

				n := 1 + r.IntN(4)
				if r.IntN(2) == 0 {
					n = 1 // About half bounce.
				}
				start := at
				event := r.IntN(8) == 0
				for i := range n {
					h := goatcounter.Hit{
						Site: siteName, CreatedAt: at, RemoteAddr: ip, UserAgentHeader: agent.ua,
						Path: entry, Ref: ref, Size: goatcounter.Floats{agent.width},
						Hostname: siteName, Language: "deu",
						Location: goatcounter.GeoLocation{Country: loc.Value, Region: loc.Value2},
					}
					if i > 0 {
						h.Path, h.Ref = pick(r, allPages).Value, "https://"+siteName+"/"
					}
					if err := goatcounter.Collect(ctx, h); err != nil {
						return err
					}
					if p, _, _ := cut(h.Path, "?"); !seenPage[p] {
						seenPage[p] = true
						day.Pages[p]++
					}
					if i < n-1 {
						at = at.Add(time.Duration(10+r.IntN(170)) * time.Second)
					}
				}
				if event {
					at = at.Add(time.Duration(5+r.IntN(30)) * time.Second)
					h := goatcounter.Hit{
						Site: siteName, CreatedAt: at, RemoteAddr: ip, UserAgentHeader: agent.ua,
						Path: "/", Hostname: siteName, Name: "Outbound Link: Click",
						Props:    `{"url":"https://www.roll-pastuch.de/"}`,
						Location: goatcounter.GeoLocation{Country: loc.Value, Region: loc.Value2},
					}
					if err := goatcounter.Collect(ctx, h); err != nil {
						return err
					}
					if !hadEvent {
						hadEvent = true
						day.Events["Outbound Link: Click"]++
					}
				}

				day.Visits++
				day.Pageviews += n
				if n == 1 && !event {
					day.Bounces++
				}
				day.Duration += int(at.Sub(start).Seconds())
				day.Browsers[agent.browser]++
				day.Countries[loc.Value]++
				day.Sources[src.Value]++
				at = at.Add(time.Duration(31+r.IntN(120)) * time.Minute) // A new visit.
			}
		}
	}
	return json.NewEncoder(os.Stdout).Encode(days)
}

func mustAtoi(s string) int {
	var n int
	fmt.Sscan(s, &n)
	return n
}

func cut(s, sep string) (string, string, bool) {
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			return s[:i], s[i+len(sep):], true
		}
	}
	return s, "", false
}

func urlEscape(s string) string {
	out := make([]byte, 0, len(s))
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
			out = append(out, c)
		default:
			out = append(out, fmt.Sprintf("%%%02X", c)...)
		}
	}
	return string(out)
}
