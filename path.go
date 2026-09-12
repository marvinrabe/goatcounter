package goatcounter

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"zgo.at/errors"
	"zgo.at/zdb"
	"zgo.at/zstd/zbool"
	"zgo.at/zstd/zreflect"
)

type PathID int32

type Path struct {
	ID    PathID     `db:"path_id,id" json:"id"` // Path ID
	Site  string     `db:"site" json:"site"`
	Path  string     `db:"path" json:"path"`   // Path name
	Event zbool.Bool `db:"event" json:"event"` // Is this an event?
}

func (Path) Table() string { return "paths" }

var _ zdb.Defaulter = &Path{}

func (p *Path) Defaults(ctx context.Context) { p.Site = MustGetSite(ctx).Key }

var _ zdb.Validator = &Path{}

func (p *Path) Validate(ctx context.Context) error {
	v := NewValidate(ctx)
	v.UTF8("path", p.Path)
	v.Len("path", p.Path, 1, 2048)
	return v.ErrorOrNil()
}

func (p *Path) ByID(ctx context.Context, id PathID) error {
	err := zdb.Get(ctx, p,
		`/* Path.ByID */ select * from paths where path_id=? and site=?`, id, MustGetSite(ctx).Key)
	return errors.Wrapf(err, "Path.ByID(%d)", id)
}

func (p *Path) ByPath(ctx context.Context, path string) error {
	err := zdb.Get(ctx, p,
		`/* Path.ByPath */ select * from paths where lower(path) = lower(?) and site=?`, path, MustGetSite(ctx).Key)
	return errors.Wrapf(err, "Path.ByPath(%q)", path)
}

func (p *Path) GetOrInsert(ctx context.Context) error {
	k := MustGetSite(ctx).Key + ":" + p.Path
	c, ok := cachePaths(ctx).Get(k)
	if ok {
		*p = c
		cachePaths(ctx).Touch(k)

		return nil
	}

	p.Defaults(ctx)
	err := p.Validate(ctx)
	if err != nil {
		return errors.Wrap(err, "Path.GetOrInsert")
	}

	err = zdb.Get(ctx, p, `/* Path.GetOrInsert */
		select * from paths
		where lower(path) = lower($1) and site = $2
		limit 1`, p.Path, MustGetSite(ctx).Key)
	if err != nil && !zdb.ErrNoRows(err) {
		return errors.Errorf("Path.GetOrInsert select: %w", err)
	}
	if err == nil {
		cachePaths(ctx).Set(k, *p)
		return nil
	}

	// Insert new path.
	err = zdb.Insert(ctx, p)
	if err != nil {
		return errors.Wrap(err, "Path.GetOrInsert insert")
	}

	// Make sure to update any filters.
	var f Filters
	err = f.List(ctx)
	if err != nil {
		return errors.Wrap(err, "Path.GetOrInsert insert")
	}
	for _, ff := range f {
		m := ff.Match(p.Path, bool(p.Event))
		if ff.Invert {
			m = !m
		}
		if m {
			err := ff.Append(ctx, p.ID)
			if err != nil {
				return errors.Wrap(err, "Path.GetOrInsert append filter")
			}
		}
	}

	cachePaths(ctx).Set(k, *p)
	return nil
}

// Merge the given paths in to this one.
func (p Path) Merge(ctx context.Context, paths Paths) error {
	pathIDs := make([]PathID, 0, len(paths))
	for _, pp := range paths {
		if pp.ID == p.ID { // Shouldn't happen, but just in case.
			return fmt.Errorf("Path.Merge: destination ID %d also in paths to merge", p.ID)
		}
		pathIDs = append(pathIDs, pp.ID)
	}

	err := zdb.TX(ctx, func(ctx context.Context) error {
		if err := clearFilters(ctx, MustGetSite(ctx).Key); err != nil {
			return err
		}
		// Update stats and counts tables
		for _, tt := range zreflect.Values(Tables, "", "") {
			var (
				t      = tt.(tbl)
				i      = slices.Index(t.Columns, "path_id")
				sel    = append([]string{}, t.Columns...)
				selCTE = append([]string{}, t.Columns...)
				group  = append([]string{}, t.Columns...)
			)

			sel[i] = ":path_id"
			selCTE = slices.Delete(selCTE, i, i+1)
			l := len(selCTE) - 1

			selCTE[l] = fmt.Sprintf("sum(%[1]s) as %[1]s", selCTE[l])

			group = slices.Delete(group, i, i+1)
			group = group[:len(group)-1]

			err := zdb.Exec(ctx, `load:paths.Merge`, map[string]any{
				"Table":      t.Table,
				"SelectCTE":  strings.Join(selCTE, ", "),
				"Select":     strings.Join(sel, ", "),
				"Columns":    strings.Join(t.Columns, ", "),
				"OnConflict": t.OnConflict(ctx),
				"Group":      strings.Join(group, ", "),
				"path_id":    p.ID,
				"paths":      pathIDs,
			})
			if err != nil {
				return err
			}
			err = zdb.Exec(ctx, `/* Path.Merge */
				delete from :tbl where path_id in (:paths)`,
				map[string]any{
					"tbl":   zdb.SQL(t.Table),
					"paths": pathIDs,
				})
			if err != nil {
				return err
			}
		}

		// Update hits and delete old paths.
		err := zdb.Exec(ctx, `/* Path.Merge */
			update hits set path_id=:path_id where path_id in (:paths)`,
			map[string]any{
				"path_id": p.ID,
				"paths":   pathIDs,
			})
		if err != nil {
			return err
		}
		return zdb.Exec(ctx, `/* Path.Merge */
			delete from paths where path_id in (:paths)`,
			map[string]any{
				"paths": pathIDs,
			})
	})
	return errors.Wrapf(err, "Path.Merge(%d, %v)", p.ID, pathIDs)
}

type Paths []Path

// List all paths.
func (p *Paths) List(ctx context.Context, after PathID, limit int) (bool, error) {
	err := zdb.Select(ctx, p, "load:paths.List", map[string]any{
		"site":  MustGetSite(ctx).Key,
		"after": after,
		"limit": limit + 1,
	})
	if err != nil {
		return false, errors.Wrap(err, "Paths.List")
	}

	more := len(*p) > limit
	if more {
		pp := *p
		pp = pp[:len(pp)-1]
		*p = pp
	}
	return more, nil
}
