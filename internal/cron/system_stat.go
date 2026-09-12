package cron

import (
	"github.com/marvinrabe/goatcounter"
)

func groupSystemStats(hits []goatcounter.Hit) statBatch {
	type key struct {
		site     string
		pathID   goatcounter.PathID
		at       string
		systemID goatcounter.SystemID
	}
	grouped := make(map[key]int)
	for _, h := range hits {
		if h.Bot > 0 || !h.FirstVisit || h.SystemID == 0 {
			continue
		}

		grouped[key{h.Site, h.PathID, h.CreatedAt.Format("2006-01-02"), h.SystemID}]++
	}
	batch := statBatch{bulk: goatcounter.Tables.SystemStats.Bulk}
	for k, count := range grouped {
		batch.rows = append(batch.rows, []any{k.site, k.pathID, k.at, k.systemID, count})
	}
	return batch
}
