package cron

import (
	"github.com/marvinrabe/goatcounter"
)

func groupSizeStats(hits []goatcounter.Hit) statBatch {
	type key struct {
		site   string
		pathID goatcounter.PathID
		at     string
		width  int
	}
	grouped := make(map[key]int)
	for _, h := range hits {
		if h.Bot > 0 || !h.FirstVisit {
			continue
		}
		var width int
		if len(h.Size) > 0 {
			width = int(h.Size[0])
		}
		grouped[key{h.Site, h.PathID, h.CreatedAt.Format("2006-01-02"), width}]++
	}
	batch := statBatch{bulk: goatcounter.Tables.SizeStats.Bulk}
	for k, count := range grouped {
		batch.rows = append(batch.rows, []any{k.site, k.pathID, k.at, k.width, count})
	}
	return batch
}
