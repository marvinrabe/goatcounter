package goatcounter_test

import (
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
