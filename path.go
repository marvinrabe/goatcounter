package goatcounter

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/validation"
)

type PathID int32

type Path struct {
	ID    PathID `db:"path_id,id" json:"id"` // Path ID
	Site  string `db:"site" json:"site"`
	Path  string `db:"path" json:"path"`   // Path name
	Event bool   `db:"event" json:"event"` // Is this an event?
}

func (Path) Table() string { return "paths" }

var _ database.Defaulter = &Path{}

func (p *Path) Defaults(ctx context.Context) { p.Site = MustGetSite(ctx).Key }

var _ database.Validator = &Path{}

func (p *Path) Validate(ctx context.Context) error {
	v := validation.New()
	v.UTF8("path", p.Path)
	v.Len("path", p.Path, 1, 2048)
	return v.ErrorOrNil()
}

func (p *Path) ByID(ctx context.Context, id PathID) error {
	err := database.Get(ctx, p,
		`/* Path.ByID */ select * from paths where path_id=? and site=?`, id, MustGetSite(ctx).Key)
	if err != nil {
		err = fmt.Errorf("Path.ByID(%d): %w", id, err)
	}
	return err
}

func (p *Path) ByPath(ctx context.Context, path string) error {
	err := database.Get(ctx, p,
		`/* Path.ByPath */ select * from paths where lower(path) = lower(?) and site=?`, path, MustGetSite(ctx).Key)
	if err != nil {
		err = fmt.Errorf("Path.ByPath(%q): %w", path, err)
	}
	return err
}

func (p *Path) GetOrInsert(ctx context.Context) error {
	k := MustGetSite(ctx).Key + ":" + p.Path
	cache := batchCacheFor(ctx).paths
	c, ok := cache[k]
	if ok {
		*p = c

		return nil
	}

	p.Defaults(ctx)
	err := p.Validate(ctx)
	if err != nil {
		return fmt.Errorf("Path.GetOrInsert: %w", err)
	}

	err = database.Get(ctx, p, `/* Path.GetOrInsert */
		select * from paths
		where lower(path) = lower(?) and site = ?
		limit 1`, p.Path, MustGetSite(ctx).Key)
	if err != nil && !database.ErrNoRows(err) {
		return fmt.Errorf("Path.GetOrInsert select: %w", err)
	}
	if err == nil {
		if cache != nil {
			cache[k] = *p
		}
		return nil
	}

	// Insert new path.
	err = database.Insert(ctx, p)
	if err != nil {
		return fmt.Errorf("Path.GetOrInsert insert: %w", err)
	}

	// Make sure to update any filters.
	var f Filters
	err = f.List(ctx)
	if err != nil {
		return fmt.Errorf("Path.GetOrInsert insert: %w", err)
	}
	for _, ff := range f {
		m := ff.Match(p.Path, bool(p.Event))
		if ff.Invert {
			m = !m
		}
		if m {
			err := ff.Append(ctx, p.ID)
			if err != nil {
				return fmt.Errorf("Path.GetOrInsert append filter: %w", err)
			}
		}
	}

	if cache != nil {
		cache[k] = *p
	}
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

	err := database.TX(ctx, func(ctx context.Context) error {
		if err := clearFilters(ctx, MustGetSite(ctx).Key); err != nil {
			return err
		}
		// Update stats and counts tables
		for i := 0; i < reflect.ValueOf(Tables).NumField(); i++ {
			tt := reflect.ValueOf(Tables).Field(i).Interface()
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

			err := database.Exec(ctx, `load:paths.Merge`, map[string]any{
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
			err = database.Exec(ctx, `/* Path.Merge */
				delete from :tbl where path_id in (:paths)`,
				map[string]any{
					"tbl":   database.SQL(t.Table),
					"paths": pathIDs,
				})
			if err != nil {
				return err
			}
		}

		// Update hits and delete old paths.
		err := database.Exec(ctx, `/* Path.Merge */
			update hits set path_id=:path_id where path_id in (:paths)`,
			map[string]any{
				"path_id": p.ID,
				"paths":   pathIDs,
			})
		if err != nil {
			return err
		}
		return database.Exec(ctx, `/* Path.Merge */
			delete from paths where path_id in (:paths)`,
			map[string]any{
				"paths": pathIDs,
			})
	})
	if err != nil {
		err = fmt.Errorf("Path.Merge(%d, %v): %w", p.ID, pathIDs, err)
	}
	return err
}

type Paths []Path

// List all paths.
func (p *Paths) List(ctx context.Context, after PathID, limit int) (bool, error) {
	err := database.Select(ctx, p, "load:paths.List", map[string]any{
		"site":  MustGetSite(ctx).Key,
		"after": after,
		"limit": limit + 1,
	})
	if err != nil {
		return false, fmt.Errorf("Paths.List: %w", err)
	}

	more := len(*p) > limit
	if more {
		pp := *p
		pp = pp[:len(pp)-1]
		*p = pp
	}
	return more, nil
}
