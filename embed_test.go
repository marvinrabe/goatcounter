package goatcounter_test

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"

	. "github.com/marvinrabe/goatcounter"
)

func TestEmbed(t *testing.T) {
	err := fstest.TestFS(DB, "db/schema.gotxt", "db/languages.sql")
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
		"assets/css/backend.css",
		"assets/js/backend.js",
		"assets/js/count.js",
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
