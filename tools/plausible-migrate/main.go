// Command plausible-migrate moves a Plausible Analytics CSV export into the
// events table. It's a one-off migration, and not part of the server.
//
//	TZ=Europe/Berlin go run ./tools/plausible-migrate -db 'libsql://…?authToken=…' -site example.com export.zip
//
// Each CSV row becomes one row with its daily totals, and aggregate set to the
// CSV's kind; see internal/database/schema.sql. Its timestamp is midnight in TZ, which must
// be the dashboard's timezone, and the timezone Plausible used for the export.
//
// Values are stored as Plausible exported them, which is how the collector
// stores new data too; only the column names are mapped to those of the
// events table. The migration runs in one transaction and replaces earlier
// migrated rows for the site; collected events are not touched.
package main

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/marvinrabe/goatcounter/internal/analytics"
	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/enrich"
)

var (
	// CSV columns stored under another name, to match the events table.
	renamed = map[string]string{
		"page": "path", "entry_page": "path", "exit_page": "path",
		"operating_system": "os",
	}
	// CSV columns that aren't stored: the collector only records the country
	// and no OS version.
	ignored = []string{"region", "city", "operating_system_version"}
	// CSV columns stored as a property in props.
	asProps = map[string]string{"link_url": "url", "path": "path"}

	dimensions = []string{
		"hostname", "path", "name", "source", "referrer",
		"utm_source", "utm_medium", "utm_campaign", "utm_content", "utm_term",
		"browser", "browser_version", "os", "device", "country",
	}
	metrics = []string{
		"visitors", "visits", "pageviews", "bounces", "visit_duration", "events",
		"entrances", "exits", "total_scroll_depth", "total_scroll_depth_visits",
		"total_time_on_page", "total_time_on_page_visits",
	}
	kinds = []string{
		"visitors", "pages", "sources", "browsers", "operating_systems", "devices",
		"locations", "entry_pages", "exit_pages", "custom_events", "custom_props",
	}
	filename = regexp.MustCompile(`^imported_([a-z_]+)_[0-9]{8}_[0-9]{8}\.csv$`)
)

func main() {
	db := flag.String("db", os.Getenv("GOATCOUNTER_DB"), "Local database path or remote libSQL URL.")
	site := flag.String("site", "", "Site name, as in GOATCOUNTER_SITES.")
	flag.Parse()
	if *db == "" || *site == "" || flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: TZ=Europe/Berlin plausible-migrate -db DB -site SITE export.zip")
		os.Exit(2)
	}
	if err := run(*db, *site, flag.Arg(0)); err != nil {
		fmt.Fprintln(os.Stderr, "plausible-migrate:", err)
		os.Exit(1)
	}
}

func run(connect, site, file string) error {
	archive, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer archive.Close()

	ctx := context.Background()
	db, err := database.Open(ctx, connect)
	if err != nil {
		return err
	}
	defer db.Close()

	if os.Getenv("TZ") == "" {
		return fmt.Errorf("TZ must be set to the dashboard's timezone, e.g. TZ=Europe/Berlin")
	}
	tz, err := analytics.LoadTimezone()
	if err != nil {
		return err
	}
	fmt.Printf("Timezone: %s\n", tz)

	counts := make(map[string]int)
	err = db.TX(ctx, func(tx *database.DB) error {
		if err := tx.Exec(ctx, `delete from events where site = ? and aggregate <> ''`, site); err != nil {
			return err
		}
		for _, f := range archive.File {
			m := filename.FindStringSubmatch(f.Name)
			if m == nil || !slices.Contains(kinds, m[1]) {
				return fmt.Errorf("unexpected file in archive: %q", f.Name)
			}
			n, err := migrateFile(ctx, tx, f, site, m[1], tz.Loc())
			if err != nil {
				return fmt.Errorf("%s: %w", f.Name, err)
			}
			counts[m[1]] += n
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, k := range kinds {
		fmt.Printf("%-18s %6d rows\n", k, counts[k])
	}
	return nil
}

func migrateFile(ctx context.Context, db *database.DB, f *zip.File, site, kind string, loc *time.Location) (int, error) {
	in, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer in.Close()
	r := csv.NewReader(in)
	header, err := r.Read()
	if err != nil {
		return 0, fmt.Errorf("read header: %w", err)
	}
	header[0] = strings.TrimPrefix(header[0], "\ufeff")
	if header[0] != "date" {
		return 0, fmt.Errorf("first column is %q, not date", header[0])
	}
	if kind == "custom_props" && (!slices.Contains(header, "property") || !slices.Contains(header, "value")) {
		return 0, fmt.Errorf("custom_props without property and value columns")
	}

	cols := append(append([]string{"site", "ts", "aggregate", "props"}, dimensions...), metrics...)
	ins, err := newBulkInsert(ctx, db, "events", cols)
	if err != nil {
		return 0, err
	}
	n := 0
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("row %d: %w", n+2, err)
		}
		day, err := time.ParseInLocation("2006-01-02", row[0], loc)
		if err != nil {
			return 0, fmt.Errorf("row %d: invalid date %q", n+2, row[0])
		}

		values := make(map[string]any, len(cols))
		props := make(map[string]string)
		for i, c := range header[1:] {
			v := row[i+1]
			switch {
			case slices.Contains(ignored, c):
			case kind == "custom_props" && (c == "property" || c == "value"):
				// Handled below.
			case kind == "custom_events" && asProps[c] != "":
				if v != "" {
					props[asProps[c]] = v
				}
			case slices.Contains(metrics, c):
				x, err := strconv.ParseInt(v, 10, 64)
				if err != nil || x < 0 {
					return 0, fmt.Errorf("row %d: invalid %s %q", n+2, c, v)
				}
				values[c] = x
			default:
				if to, ok := renamed[c]; ok {
					c = to
				}
				if !slices.Contains(dimensions, c) {
					return 0, fmt.Errorf("unknown column %q", header[i+1])
				}
				// Plausible tells laptops from desktops by the screen width, which
				// the collector doesn't record.
				if c == "device" && v == "Laptop" {
					v = enrich.DeviceDesktop
				}
				values[c] = v
			}
		}
		if kind == "custom_props" {
			props[row[slices.Index(header, "property")]] = row[slices.Index(header, "value")]
		}
		values["props"] = ""
		if len(props) > 0 {
			b, _ := json.Marshal(props)
			values["props"] = string(b)
		}
		values["site"], values["ts"], values["aggregate"] = site, day.Unix(), kind

		args := make([]any, len(cols))
		for i, c := range cols {
			args[i] = values[c]
			if args[i] == nil {
				args[i] = ""
				if slices.Contains(metrics, c) {
					args[i] = 0
				}
			}
		}
		ins.Values(args...)
		n++
	}
	return n, ins.Finish()
}
