package cron

import (
	"github.com/marvinrabe/goatcounter"
)

func groupLocationStats(hits []goatcounter.Hit) statBatch {
	type key struct {
		site     string
		pathID   goatcounter.PathID
		at       string
		location string
	}
	grouped := make(map[key]int)
	for _, h := range hits {
		if h.Bot > 0 || !h.FirstVisit {
			continue
		}

		grouped[key{h.Site, h.PathID, h.CreatedAt.Format("2006-01-02"), h.Location}]++
	}
	batch := statBatch{bulk: goatcounter.Tables.LocationStats.Bulk}
	for k, count := range grouped {
		batch.rows = append(batch.rows, []any{k.site, k.pathID, k.at, k.location, count})
	}
	return batch
}
