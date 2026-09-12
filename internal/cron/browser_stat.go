package cron

import (
	"github.com/marvinrabe/goatcounter"
)

func groupBrowserStats(hits []goatcounter.Hit) statBatch {
	type key struct {
		site      string
		pathID    goatcounter.PathID
		at        string
		browserID goatcounter.BrowserID
	}
	grouped := make(map[key]int)
	for _, h := range hits {
		if h.Bot > 0 || !h.FirstVisit || h.BrowserID == 0 {
			continue
		}

		grouped[key{h.Site, h.PathID, h.CreatedAt.Format("2006-01-02"), h.BrowserID}]++
	}
	batch := statBatch{bulk: goatcounter.Tables.BrowserStats.Bulk}
	for k, count := range grouped {
		batch.rows = append(batch.rows, []any{k.site, k.pathID, k.at, k.browserID, count})
	}
	return batch
}
