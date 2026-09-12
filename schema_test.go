package goatcounter_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	. "github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"zgo.at/zdb"
)

func TestStatsRejectInvalidDates(t *testing.T) {
	ctx := testenv.DB(t)
	for _, table := range []struct {
		name, dimensions, values string
	}{
		{"browser_stats", "browser_id", "1"},
		{"system_stats", "system_id", "1"},
		{"location_stats", "location", "''"},
		{"language_stats", "language", "''"},
		{"size_stats", "width", "0"},
		{"campaign_stats", "campaign_id, ref", "1, ''"},
	} {
		t.Run(table.name, func(t *testing.T) {
			query := fmt.Sprintf(`insert into %s (site, path_id, day, count, %s) values ('example.com', 1, ?, 1, %s)`, table.name, table.dimensions, table.values)
			for _, day := range []string{"2024-02-29", "2026-09-12"} {
				if err := zdb.Exec(ctx, query, day); err != nil {
					t.Errorf("valid date %q rejected: %v", day, err)
				}
			}
			for _, day := range []any{nil, "", "not-a-date", "2026-02-29", "2026-02-30", "2026-13-01", "2026-09-31", "2026-9-12", "2026-09-12 00:00:00", "2026-09-12T00:00:00Z"} {
				if err := zdb.Exec(ctx, query, day); err == nil {
					t.Errorf("invalid date %v accepted", day)
				}
			}
		})
	}
}

func TestDashboardQueriesUseTimeIndexes(t *testing.T) {
	ctx := testenv.DB(t)
	start := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	// Give the planner a realistic history: a one-day query should read a
	// narrow date range rather than all of a site's rows in grouping order.
	if err := zdb.Exec(ctx, `insert into paths (path_id, site, path) values (1, 'example.com', '/'), (2, 'other.example', '/')`); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`insert into hits (site, path_id, browser_id, system_id, created_at) select site, path_id, 0, 0, hour from history`,
		`insert into hit_counts (site, path_id, hour, total) select site, path_id, hour, 1 from history`,
		`insert into ref_counts (site, path_id, ref_id, hour, total) select site, path_id, 1, hour, 1 from history`,
	} {
		if err := zdb.Exec(ctx, `with recursive hours(n) as (
			values (0) union all select n + 1 from hours where n < 20000
		), history as (
			select case n % 2 when 0 then 'example.com' else 'other.example' end as site,
				n % 2 + 1 as path_id, datetime('2025-01-01', '+' || n || ' hours') as hour
			from hours
		) `+query); err != nil {
			t.Fatal(err)
		}
	}
	if err := zdb.Exec(ctx, `analyze`); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		query, table, index string
	}{
		{"dashboard_metrics.Get", "hits", "hits#site#created_at"},
		{"dashboard_metrics.Series", "hits", "hits#site#created_at"},
		{"dashboard_metrics.Pageviews", "hits", "hits#site#created_at"},
		{"hit_list.List", "hit_counts", "hit_counts#site#hour"},
		{"hit_list.Totals", "hit_counts", "hit_counts#site#hour"},
		{"hit_list.GetTotalCount", "hit_counts", "hit_counts#site#hour"},
		{"ref.ListTopRefs", "ref_counts", "ref_counts#site#hour"},
		{"hit_stats.ByRef", "ref_counts", "ref_counts#site#hour"},
	} {
		t.Run(tt.query, func(t *testing.T) {
			filter, params := (PathFilter{}).SQL(ctx, tt.table)
			params["filter"] = filter
			params["start"], params["start_utc"] = start, start
			params["end"], params["end_utc"] = start.Add(24*time.Hour), start.Add(24*time.Hour)
			params["sqlite"] = true
			params["offset"], params["offset2"] = 0, "0 minutes"
			params["limit"], params["limit2"] = 10, 40
			params["total_events"], params["no_events"] = true, true
			params["exclude"], params["has_domain"] = []PathID{}, false
			params["ref"] = "example.org"
			query, isTemplate, err := zdb.Load(zdb.MustGetDB(ctx), tt.query)
			if err != nil {
				t.Fatal(err)
			}
			if isTemplate {
				rendered, err := zdb.Template(zdb.DialectSQLite, query, params)
				if err != nil {
					t.Fatal(err)
				}
				query = string(rendered)
			}
			var plan []struct {
				ID     int    `db:"id"`
				Parent int    `db:"parent"`
				Unused int    `db:"notused"`
				Detail string `db:"detail"`
			}
			if err := zdb.Select(ctx, &plan, "explain query plan "+query, params); err != nil {
				t.Fatal(err)
			}
			var found bool
			for _, step := range plan {
				if strings.Contains(step.Detail, tt.index) && strings.Contains(step.Detail, "site=? AND <expr>>? AND <expr><?") {
					found = true
				}
				if strings.HasPrefix(step.Detail, "SCAN "+tt.table) {
					t.Errorf("full table scan: %s", step.Detail)
				}
			}
			if !found {
				t.Errorf("no indexed site/time range lookup: %+v", plan)
			}
		})
	}
}
