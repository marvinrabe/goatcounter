package goatcounter

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"strings"
	"time"

	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/datetime"
)

type FilterID int32

type Filter struct {
	FilterID   FilterID  `db:"filter_id,readonly"`
	Site       string    `db:"site"`
	Matches    int       `db:"matches"`
	Invert     bool      `db:"invert"`
	Query      string    `db:"query"`
	CreatedAt  time.Time `db:"created_at,readonly"`
	LastUsedAt time.Time `db:"last_used_at,readonly"`
}

func (f Filter) Table() string { return "filters" }

var _ database.Defaulter = &Filter{}

func (f *Filter) Defaults(ctx context.Context) {
	f.Site = MustGetSite(ctx).Key
	for f.FilterID == 0 {
		f.FilterID = FilterID(rand.Int32())
		if rand.IntN(2) == 1 {
			f.FilterID = -f.FilterID
		}
	}
	if f.CreatedAt.IsZero() {
		f.CreatedAt = datetime.Now(ctx)
	}
	if f.LastUsedAt.IsZero() {
		f.LastUsedAt = datetime.Now(ctx)
	}
}

func (f *Filter) ByQuery(ctx context.Context, query string) error {
	err := database.Get(ctx, f, `select * from filters where site=? and lower(query)=lower(?)`, MustGetSite(ctx).Key, query)
	if err != nil {
		err = fmt.Errorf("Filter.ByQuery(%q): %w", query, err)
	}
	return err
}

func (f Filter) Touch(ctx context.Context) error {
	if f.FilterID == 0 {
		return errors.New("Filter.Touch: ID==0")
	}
	now := datetime.Now(ctx)
	if now.Sub(f.LastUsedAt) < time.Hour {
		return nil
	}
	err := database.Exec(ctx, `update filters set last_used_at=? where filter_id=? and site=?
		and datetime(last_used_at) <= datetime(?)`, now, f.FilterID, MustGetSite(ctx).Key, now.Add(-time.Hour))
	if err != nil {
		err = fmt.Errorf("Filter.Touch(%d): %w", f.FilterID, err)
	}
	return err
}

func (f *Filter) Insert(ctx context.Context, paths []PathID) error {
	f.Matches = len(paths)
	err := database.TX(ctx, func(ctx context.Context) error {
		err := database.Insert(ctx, f)
		if err != nil {
			return err
		}

		b, err := database.NewBulkInsert(ctx, "filter_paths", []string{"filter_id", "path_id"})
		if err != nil {
			return err
		}
		for _, p := range paths {
			b.Values(f.FilterID, p)
		}
		return b.Finish()
	})
	if err != nil {
		err = fmt.Errorf("Filter.Insert: %w", err)
	}
	return err
}

func (f Filter) Append(ctx context.Context, id PathID) error {
	if f.FilterID == 0 {
		return errors.New("Filter.Append: id is 0")
	}
	err := database.TX(ctx, func(ctx context.Context) error {
		err := database.Exec(ctx, `insert into filter_paths (filter_id, path_id) values (?, ?)`, f.FilterID, id)
		if err != nil {
			return err
		}
		return database.Exec(ctx, `update filters set matches = matches + 1 where filter_id = ? and site = ?`, f.FilterID, MustGetSite(ctx).Key)
	})
	if err != nil {
		err = fmt.Errorf("Filter.Append: %w", err)
	}
	return err
}

func (f Filter) Match(path string, event bool) bool {
	like, kw := findFilter(f.Query,
		"at:start", "at:end", "is:event", "is:pageview", "in:path", ":not")
	like = strings.ToLower(regexp.QuoteMeta(like))
	var not bool
	for _, f := range kw {
		switch f {
		case "at:start":
			like = "^" + like
		case "at:end":
			like = like + "$"
		case "is:event":
			if !event {
				return false
			}
		case "is:pageview":
			if event {
				return false
			}
		case ":not":
			not = true
		}
	}

	re, err := regexp.Compile(like)
	if err != nil {
		return false
	}
	match := re.MatchString(path)
	if not {
		match = !match
	}
	return match
}

type Filters []Filter

func (f *Filters) List(ctx context.Context) error {
	err := database.Select(ctx, f, `select * from filters where site=?`, MustGetSite(ctx).Key)
	if err != nil {
		err = fmt.Errorf("Filters.List: %w", err)
	}
	return err
}

// clearFilters invalidates cached path selections after a merge or deletion.
// Call inside the transaction that changes the paths.
func clearFilters(ctx context.Context, site string) error {
	if err := database.Exec(ctx, `delete from filter_paths where filter_id in
		(select filter_id from filters where site=?)`, site); err != nil {
		return err
	}
	return database.Exec(ctx, `delete from filters where site=?`, site)
}

type PathFilter struct {
	ids      []PathID
	filterID FilterID
	invert   bool
}

// SQL filters the named table directly so SQLite can use its site/time index.
// The table name must be a constant from the query, never user input.
func (p PathFilter) SQL(ctx context.Context, table string) (database.SQL, map[string]any) {
	siteSQL := table + ".site = :site"
	pathID := table + ".path_id"
	withSite := func(sql database.SQL, params map[string]any) (database.SQL, map[string]any) {
		if params == nil {
			params = make(map[string]any)
		}
		params["site"] = MustGetSite(ctx).Key
		return database.SQL(siteSQL + " and (" + string(sql) + ")"), params
	}
	if p.filterID != 0 {
		if p.invert {
			return withSite(database.SQL(pathID+" not in (select path_id from filter_paths where filter_id = :filter_id)"), map[string]any{"filter_id": p.filterID})
		}
		return withSite(database.SQL(pathID+" in (select path_id from filter_paths where filter_id = :filter_id)"), map[string]any{"filter_id": p.filterID})
	}
	if len(p.ids) == 0 {
		return withSite("1=1", nil)
	}
	if p.invert {
		return withSite(database.SQL(pathID+" not in (:paths)"), map[string]any{"paths": p.ids})
	}
	return withSite(database.SQL(pathID+" in (:paths)"), map[string]any{"paths": p.ids})
}

func PathFilterFromIDs(ids []PathID) PathFilter {
	return PathFilter{ids: ids}
}

func PathFilterFromQuery(ctx context.Context, query string) (PathFilter, error) {
	if strings.TrimSpace(query) == "" {
		return PathFilter{}, nil
	}

	// Reuse persisted path IDs before scanning paths, including the original
	// inversion choice. Recently used filters need no timestamp write.
	var cached Filter
	if err := cached.ByQuery(ctx, query); err == nil {
		if err := cached.Touch(ctx); err != nil {
			return PathFilter{}, err
		}
		return PathFilter{filterID: cached.FilterID, invert: cached.Invert}, nil
	} else if !database.ErrNoRows(err) {
		if err != nil {
			err = fmt.Errorf("PathFilter: %w", err)
		}
		return PathFilter{}, err
	}

	like, kw := findFilter(strings.ReplaceAll(query, "%", "%%"),
		"at:start", "at:end", "is:event", "is:pageview", "in:path", ":not")
	var (
		onlyEvent, onlyPageview, atStart, atEnd bool
		not                                     database.SQL
	)
	for _, f := range kw {
		switch f {
		case "at:start":
			atStart = true
		case "at:end":
			atEnd = true
		case "is:event":
			onlyEvent = true
		case "is:pageview":
			onlyPageview = true
		case ":not":
			not = "not"
		}
	}
	haveLike := like != ""
	if !atEnd {
		like = like + "%"
	}
	if !atStart {
		like = "%" + like
	}

	getPathIDs := func(scan any, invert bool) error {
		return database.Select(ctx, scan, "load:paths.PathFilter", map[string]any{
			"site":          MustGetSite(ctx).Key,
			"like":          like,
			"have_like":     haveLike,
			"only_event":    onlyEvent,
			"only_pageview": onlyPageview,
			"not":           not,
			"invert":        invert,
		})
	}

	var pathIDs []PathID
	err := getPathIDs(&pathIDs, false)
	if err != nil {
		return PathFilter{}, fmt.Errorf("PathFilter: %w", err)
	}

	var invert bool
	// If there's tons of matches then check if we can invert the match. "List
	// all except these 10,000" is lots faster than "include these 500,000
	// paths".
	// Only worth the extra query to count the inverse once the include list is
	// large.
	if len(pathIDs) > 100_000 {
		var invertIDs []PathID
		err := getPathIDs(&invertIDs, true)
		if err != nil {
			return PathFilter{}, fmt.Errorf("PathFilter: %w", err)
		}
		if len(pathIDs) > len(invertIDs) {
			pathIDs, invert = invertIDs, true
		}
	}

	// For SQLite we want to use a join as it limits the parameters to 32k. In
	// hit_list.GetTotalCount it uses the path lists three times, so more than
	// ~10k paths will error out.
	//
	m := 10_000
	filter := Filter{Query: query, Invert: invert}
	if len(pathIDs) > m {
		err := database.TX(ctx, func(ctx context.Context) error {
			err := filter.ByQuery(ctx, query)
			if err != nil {
				if database.ErrNoRows(err) {
					return filter.Insert(ctx, pathIDs)
				}
				return err
			}
			return filter.Touch(ctx)
		})
		if err != nil {
			return PathFilter{}, fmt.Errorf("PathFilter: %w", err)
		}
		pathIDs = pathIDs[:0]
		invert = filter.Invert
	}

	return PathFilter{filterID: filter.FilterID, ids: pathIDs, invert: invert}, nil
}

func findFilter(filter string, find ...string) (string, []string) {
	found := make([]string, 0, 2)
	for _, f := range find {
		if i := strings.Index(filter, f); i > -1 {
			filter = strings.TrimSpace(filter[:i] + filter[i+len(f):])
			found = append(found, f)
		}
	}
	return filter, found
}
