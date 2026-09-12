package cron

import (
	"context"
	"strconv"

	"github.com/marvinrabe/goatcounter"
	"zgo.at/errors"
	"zgo.at/zdb"
	"zgo.at/zstd/ztype"
)

func updateLanguageStats(ctx context.Context, hits []goatcounter.Hit) error {
	err := zdb.TX(ctx, func(ctx context.Context) error {
		type gt struct {
			count    int
			day      string
			language string
			pathID   goatcounter.PathID
		}
		grouped := map[string]gt{}
		for _, h := range hits {
			if h.Bot > 0 {
				continue
			}

			day := h.CreatedAt.Format("2006-01-02")
			lang := ztype.Deref(h.Language, "")
			k := day + lang + strconv.Itoa(int(h.PathID))
			v := grouped[k]
			if v.count == 0 {
				v.day = day
				v.language = lang
				v.pathID = h.PathID
			}

			if h.FirstVisit {
				v.count += 1
			}
			grouped[k] = v
		}

		ins, err := goatcounter.Tables.LanguageStats.Bulk(ctx)
		if err != nil {
			return err
		}

		for _, v := range grouped {
			if v.count > 0 {
				ins.Values(v.pathID, v.day, v.language, v.count)
			}
		}
		return ins.Finish()
	})
	return errors.Wrap(err, "cron.updateLanguageStats")
}
