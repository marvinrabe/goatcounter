package goatcounter_test

import (
	"context"
	"errors"
	"testing"

	. "github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"zgo.at/zdb"
)

func TestCampaignCache(t *testing.T) {
	ctx := testenv.DB(t)
	if err := zdb.Exec(ctx, `insert into campaigns (name) values ('Alpha'), ('Beta')`); err != nil {
		t.Fatal(err)
	}
	ctx, queries := testenv.CountInlineQueries(ctx)
	var c Campaign
	for _, name := range []string{"Alpha", "Beta", "ALPHA", "beta"} {
		if err := c.ByName(ctx, name); err != nil {
			t.Fatal(err)
		}
		want := CampaignID(1)
		if name == "Beta" || name == "beta" {
			want = 2
		}
		if c.ID != want {
			t.Fatalf("%s returned campaign %d, want %d", name, c.ID, want)
		}
		c.Name, c.ID = "mutated receiver", 999
	}
	if n := queries.Count(`select * from campaigns where lower(name)=lower(?)`); n != 2 {
		t.Fatalf("campaign reads = %d, want 2", n)
	}

	c = Campaign{Name: "Gamma"}
	if err := c.Insert(ctx); err != nil {
		t.Fatal(err)
	}
	c = Campaign{}
	if err := c.ByName(ctx, "GAMMA"); err != nil || c.ID != 3 {
		t.Fatalf("inserted campaign: %#v, %v", c, err)
	}
	if n := queries.Count(`select * from campaigns where lower(name)=lower(?)`); n != 2 {
		t.Fatalf("newly inserted campaign required another read: %d", n)
	}
}

func TestCampaignCacheRollback(t *testing.T) {
	ctx := testenv.DB(t)
	abort := errors.New("abort import")
	err := zdb.TX(ctx, func(ctx context.Context) error {
		c := Campaign{Name: "rolled back"}
		if err := c.Insert(ctx); err != nil {
			return err
		}
		if err := c.ByName(ctx, c.Name); err != nil {
			return err
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatal(err)
	}
	var c Campaign
	if err := c.ByName(ctx, "rolled back"); !zdb.ErrNoRows(err) {
		t.Fatalf("uncommitted campaign escaped into cache: %#v, %v", c, err)
	}
}
