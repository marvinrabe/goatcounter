package cron

import (
	"context"
	"log/slog"
	"time"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/db2"
	"github.com/marvinrabe/goatcounter/internal/log"
	"zgo.at/errors"
	"zgo.at/zdb"
	"zgo.at/zstd/ztime"
)

func oldBot(ctx context.Context) error {
	ival := goatcounter.Interval(ctx, 30)
	err := zdb.Exec(ctx, `delete from bots where created_at < `+ival)
	if err != nil {
		log.Module("cron").Error(ctx, err)
	}
	return nil
}

func persistAndStat(ctx context.Context) error {
	l := log.Module("cron")
	l.Debug(ctx, "persistAndStat started")

	start := ztime.Now(ctx)
	hits, err := goatcounter.Memstore.Persist(ctx)
	if err != nil {
		return err
	}
	tookMemstore := time.Since(start).Round(time.Millisecond)

	startStats := ztime.Now(ctx)
	stat := make([]goatcounter.Hit, 0, len(hits))
	for _, h := range hits {
		if h.Bot > 0 {
			continue
		}
		stat = append(stat, h)
	}
	if len(stat) > 0 {
		if err := UpdateStats(ctx, stat); err != nil {
			l.Error(ctx, err, "paths", stat)
		}
	}

	if len(hits) > 0 {
		l.Debug(ctx, "persisted hits",
			"num", len(hits),
			slog.Group("took",
				"memstore", tookMemstore,
				"stats", time.Since(startStats).Round(time.Millisecond),
			))
	}
	return err
}

// UpdateStats updates all the stats tables.
//
// Exported for tests.
func UpdateStats(ctx context.Context, hits []goatcounter.Hit) error {
	site := goatcounter.GetSite(ctx)
	if site == nil {
		site = new(goatcounter.Site)
		if err := site.Load(ctx); err != nil {
			return err
		}
		ctx = goatcounter.WithSite(ctx, site)
	}

	funs := []func(context.Context, []goatcounter.Hit) error{
		updateHitCounts,
		updateRefCounts,
		updateBrowserStats,
		updateSystemStats,
		updateLocationStats,
		updateLanguageStats,
		updateSizeStats,
		updateCampaignStats,
	}

	for _, f := range funs {
		if err := f(ctx, hits); err != nil {
			return err
		}
	}

	if !site.ReceivedData {
		err := site.UpdateReceivedData(ctx)
		if err != nil {
			return errors.Wrap(err, "update received_data")
		}
	}
	return nil
}

func oldFilters(ctx context.Context) error {
	return zdb.TX(ctx, func(ctx context.Context) error {
		var (
			ival = goatcounter.Interval(ctx, 2)
			ids  []goatcounter.FilterID
		)
		err := zdb.Select(ctx, &ids, `delete from filters where last_used_at < `+ival+` returning filter_id`)
		if err != nil {
			return err
		}

		if len(ids) > 0 {
			return zdb.Exec(ctx, `delete from filter_paths where filter_id :in (:ids)`, map[string]any{
				"in":  db2.In(ctx),
				"ids": db2.Array(ctx, ids),
			})
		}
		return nil
	})
}

func sessions(ctx context.Context) error {
	goatcounter.Memstore.EvictSessions(ctx)
	return nil
}
