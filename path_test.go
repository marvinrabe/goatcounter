package goatcounter_test

import (
	"testing"

	. "github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/testenv"
)

func TestPathsGetOrInsert(t *testing.T) {
	ctx := testenv.DB(t)

	p := Path{Path: "/x"}
	err := p.GetOrInsert(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for range 2 {
		p2 := Path{Path: "/x"}
		err := p2.GetOrInsert(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if p2.ID != p.ID {
			t.Fatalf("wrong ID: %d", p2.ID)
		}
	}
}
