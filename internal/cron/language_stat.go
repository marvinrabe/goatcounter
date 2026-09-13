package cron

import (
	"github.com/marvinrabe/goatcounter"
)

func groupLanguageStats(hits []goatcounter.Hit) statBatch {
	type key struct {
		site     string
		pathID   goatcounter.PathID
		at       string
		language string
	}
	grouped := make(map[key]int)
	for _, h := range hits {
		if h.Bot > 0 || !h.FirstVisit {
			continue
		}

		language := ""
		if h.Language != nil {
			language = *h.Language
		}
		grouped[key{h.Site, h.PathID, h.CreatedAt.Format("2006-01-02"), language}]++
	}
	batch := statBatch{bulk: goatcounter.Tables.LanguageStats.Bulk}
	for k, count := range grouped {
		batch.rows = append(batch.rows, []any{k.site, k.pathID, k.at, k.language, count})
	}
	return batch
}
