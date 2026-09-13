package cron

import (
	"context"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/log"
	"zgo.at/errors"
	"zgo.at/zdb"
)

func oldBot(ctx context.Context) error {
	err := zdb.Exec(ctx, `delete from bots where created_at < datetime('now', '-30 days')`)
	if err != nil {
		log.Module("cron").Error(ctx, err)
	}
	return nil
}

// PersistAndStat drains bounded batches, with a transaction per batch.
func PersistAndStat(ctx context.Context) error {
	for range 16 {
		hits, err := goatcounter.PersistHits(ctx, UpdateStats)
		if err != nil {
			return err
		}
		if len(hits) < goatcounter.HitBatchSize {
			return nil
		}
	}
	return nil
}

// UpdateStats updates all the stats tables.
//
// Exported for tests.
func UpdateStats(ctx context.Context, hits []goatcounter.Hit) error {
	batches := []statBatch{
		groupHitCounts(hits), groupRefCounts(hits),
		groupBrowserStats(hits), groupSystemStats(hits),
		groupLocationStats(hits), groupLanguageStats(hits),
		groupSizeStats(hits), groupCampaignStats(hits),
	}
	if len(batches[0].rows) == 0 {
		return nil
	}

	// Ensure each location exists once per batch, before taking the aggregate
	// write lock. Location.ByCode caches these dimension rows.
	locations := make(map[string]bool)
	for _, h := range hits {
		if h.Bot > 0 || !bool(h.FirstVisit) || locations[h.Location] {
			continue
		}
		if err := (&goatcounter.Location{}).ByCode(ctx, h.Location); err != nil {
			return errors.Wrap(err, "UpdateStats location")
		}
		locations[h.Location] = true
	}

	return zdb.TX(ctx, func(ctx context.Context) error {
		for _, batch := range batches {
			if len(batch.rows) == 0 {
				continue
			}
			ins, err := batch.bulk(ctx)
			if err != nil {
				return err
			}
			for _, row := range batch.rows {
				ins.Values(row...)
			}
			if err := ins.Finish(); err != nil {
				return errors.Wrap(err, "UpdateStats")
			}
		}
		return nil
	})
}

// statBatch holds already grouped rows; constructing it performs no SQL.
type statBatch struct {
	bulk func(context.Context) (zdb.BulkInsert, error)
	rows [][]any
}

func oldFilters(ctx context.Context) error {
	return zdb.TX(ctx, func(ctx context.Context) error {
		var ids []goatcounter.FilterID
		err := zdb.Select(ctx, &ids,
			`delete from filters where last_used_at < datetime('now', '-2 days') returning filter_id`)
		if err != nil {
			return err
		}

		if len(ids) > 0 {
			return zdb.Exec(ctx, `delete from filter_paths where filter_id in (:ids)`, map[string]any{
				"ids": ids,
			})
		}
		return nil
	})
}

func sessions(ctx context.Context) error {
	return goatcounter.EvictSessions(ctx)
}
