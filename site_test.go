package goatcounter_test

import (
	"testing"

	. "github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/testenv"
)

func TestSiteLoad(t *testing.T) {
	ctx := testenv.DB(t)

	var s Site
	if err := s.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if s.CreatedAt.IsZero() {
		t.Error("created_at is zero")
	}
	if s.Settings.Public != "private" {
		t.Errorf("default settings not applied: %#v", s.Settings)
	}
}

func TestSiteUpdate(t *testing.T) {
	ctx := testenv.DB(t)

	var s Site
	if err := s.Load(ctx); err != nil {
		t.Fatal(err)
	}

	s.LinkDomain = "example.com"
	s.Settings.Public = "public"
	if err := s.Update(ctx); err != nil {
		t.Fatal(err)
	}

	// Re-read from the database; Update() clears the cache.
	var s2 Site
	if err := s2.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if s2.LinkDomain != "example.com" {
		t.Errorf("link_domain not stored: %q", s2.LinkDomain)
	}
	if !s2.Settings.IsPublic() {
		t.Errorf("settings not stored: %#v", s2.Settings)
	}
}

func TestSiteValidate(t *testing.T) {
	ctx := testenv.DB(t)

	s := Site{LinkDomain: "not a url"}
	s.Defaults(ctx)
	if err := s.Validate(ctx); err == nil {
		t.Error("no error for an invalid link_domain")
	}
}
