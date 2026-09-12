package cron

import (
	"github.com/marvinrabe/goatcounter"
)

func groupCampaignStats(hits []goatcounter.Hit) statBatch {
	type key struct {
		site       string
		pathID     goatcounter.PathID
		at         string
		campaignID goatcounter.CampaignID
		ref        string
	}
	grouped := make(map[key]int)
	for _, h := range hits {
		if h.Bot > 0 || !h.FirstVisit || h.CampaignID == nil || *h.CampaignID == 0 {
			continue
		}

		grouped[key{h.Site, h.PathID, h.CreatedAt.Format("2006-01-02"), *h.CampaignID, h.Ref}]++
	}
	batch := statBatch{bulk: goatcounter.Tables.CampaignStats.Bulk}
	for k, count := range grouped {
		batch.rows = append(batch.rows, []any{k.site, k.pathID, k.at, k.campaignID, k.ref, count})
	}
	return batch
}
