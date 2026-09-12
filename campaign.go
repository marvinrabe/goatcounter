package goatcounter

import (
	"context"
	"strings"

	"zgo.at/errors"
	"zgo.at/zdb"
	"zgo.at/zvalidate"
)

type CampaignID int32

type Campaign struct {
	ID   CampaignID `db:"campaign_id,id" json:"campaign_id"`
	Name string     `db:"name" json:"name"`
}

func (Campaign) Table() string { return "campaigns" }

var _ zdb.Defaulter = &Campaign{}

func (c *Campaign) Defaults(ctx context.Context) {}

var _ zdb.Validator = &Campaign{}

func (c *Campaign) Validate(ctx context.Context) error {
	v := zvalidate.New()
	v.Required("name", c.Name)
	return v.ErrorOrNil()
}

func (c *Campaign) Insert(ctx context.Context) error {
	err := zdb.Insert(ctx, c)
	if err == nil {
		c.cache(ctx)
	}
	return errors.Wrap(err, "Campaign.Insert")
}

func (c *Campaign) ByName(ctx context.Context, name string) error {
	k := strings.ToLower(name)
	if cc, ok := cacheCampaigns(ctx).Get(k); ok {
		*c = *cc
		return nil
	}

	err := zdb.Get(ctx, c, `select * from campaigns where lower(name)=lower(?)`, name)
	if err != nil {
		return errors.Wrap(err, "Campaign.ByName")
	}

	c.cache(ctx)
	return nil
}

// Store a copy so reusing a receiver cannot change another campaign's entry.
// Uncommitted inserts must never escape into the shared cache.
func (c Campaign) cache(ctx context.Context) {
	if _, tx := zdb.DBSQL(ctx); tx == nil {
		cacheCampaigns(ctx).Set(strings.ToLower(c.Name), &c)
	}
}
