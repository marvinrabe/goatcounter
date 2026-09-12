package goatcounter_test

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	. "github.com/marvinrabe/goatcounter"
)

func TestEmbed(t *testing.T) {
	err := fstest.TestFS(DB, "db/schema.gotxt", "db/languages.sql",
		"db/migrate/2026-09-11-1-single-site.sql", "db/migrate/2026-09-12-1-languages.gotxt")
	if err != nil {
		t.Fatal(err)
	}

	err = fstest.TestFS(DB, "db/goatcounter.sqlite3")
	if err == nil {
		t.Fatal("db/goatcounter.sqlite3 in embeded files")
	}
}

func TestAssets(t *testing.T) {
	assets, err := AssetPaths(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	for _, source := range []string{
		"assets/backend.css",
		"assets/backend.js",
		"assets/count.js",
	} {
		built, ok := assets[source]
		if !ok {
			t.Errorf("%s is missing from Vite manifest", source)
			continue
		}
		if _, err := fs.Stat(Static, "public/"+built); err != nil {
			t.Errorf("%s: %v", source, err)
		}
	}
}
