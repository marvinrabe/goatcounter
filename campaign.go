package goatcounter

import (
	"context"
	"fmt"
	"strings"

	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/validation"
)

type CampaignID int32

type Campaign struct {
	ID   CampaignID `db:"campaign_id,id" json:"campaign_id"`
	Name string     `db:"name" json:"name"`
}

func (Campaign) Table() string { return "campaigns" }

var _ database.Defaulter = &Campaign{}

func (c *Campaign) Defaults(ctx context.Context) {}

var _ database.Validator = &Campaign{}

func (c *Campaign) Validate(ctx context.Context) error {
	v := validation.New()
	v.Required("name", c.Name)
	return v.ErrorOrNil()
}

func (c *Campaign) Insert(ctx context.Context) error {
	err := database.Insert(ctx, c)
	if err == nil {
		c.cache(ctx)
	}
	if err != nil {
		err = fmt.Errorf("Campaign.Insert: %w", err)
	}
	return err
}

func (c *Campaign) ByName(ctx context.Context, name string) error {
	k := strings.ToLower(name)
	if cc, ok := batchCacheFor(ctx).campaigns[k]; ok {
		*c = cc
		return nil
	}

	err := database.Get(ctx, c, `select * from campaigns where lower(name)=lower(?)`, name)
	if err != nil {
		return fmt.Errorf("Campaign.ByName: %w", err)
	}

	c.cache(ctx)
	return nil
}

// Store a copy so reusing a receiver cannot change another campaign's entry.
// Campaign IDs are only reused within the current transaction.
func (c Campaign) cache(ctx context.Context) {
	if cache := batchCacheFor(ctx).campaigns; cache != nil {
		cache[strings.ToLower(c.Name)] = c
	}
}
