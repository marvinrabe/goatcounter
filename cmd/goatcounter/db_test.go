package main

import (
	"strings"
	"testing"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"zgo.at/zdb"
	"zgo.at/zli"
	"zgo.at/zstd/ztime"
)

func TestDBSchema(t *testing.T) {
	exit, _, out := zli.Test(t)

	runCmd(t, exit, "db", "schema-sqlite")
	wantExit(t, exit, out, 0)
	if len(out.String()) < 1_000 {
		t.Error(out.String())
	}
	out.Reset()

}

func TestDBTest(t *testing.T) {
	exit, _, out, _, dbc := startTest(t)

	runCmd(t, exit, "db", "test", "-db="+dbc)
	wantExit(t, exit, out, 0)
	if !strings.Contains(out.String(), "seems okay") {
		t.Error(out.String())
	}
	out.Reset()

	doesntexist := dbc[:strings.Index(dbc, "+")+1] + "yeah_nah_doesnt_exist"

	runCmd(t, exit, "db", "test", "-db="+doesntexist)
	wantExit(t, exit, out, 1)
	if !strings.Contains(out.String(), `doesn't exist`) {
		t.Error(out.String())
	}
}

func TestDBQuery(t *testing.T) {
	exit, _, out, ctx, dbc := startTest(t)
	ctx = ztime.WithNow(ctx, ztime.FromString("2020-06-18"))

	runCmd(t, exit, "db", "query", "-db="+dbc, "select count(*) as versions from version")
	wantExit(t, exit, out, 0)

	want := `
		versions
		6`
	if d := zdb.Diff(out.String(), want); d != "" {
		t.Error(d)
	}
	out.Reset()

	testenv.StoreHits(ctx, t, false, goatcounter.Hit{
		FirstVisit:      true,
		UserAgentHeader: "Mozilla/5.0 (X11; Linux x86_64; rv:79.0) Gecko/20100101 Firefox/79.0",
	})
}

func TestDBNewDB(t *testing.T) {
	exit, _, out, _, dbc := startTest(t)

	runCmd(t, exit, "db", "newdb", "-db="+dbc)
	wantExit(t, exit, out, 2)

	tmp := t.TempDir()
	dbc = "sqlite3+" + tmp + "/new"

	runCmd(t, exit, "db", "newdb", "-db="+dbc)
	wantExit(t, exit, out, 0)

	runCmd(t, exit, "db", "newdb", "-db="+dbc)
	wantExit(t, exit, out, 2)
}

func TestDBMigrate(t *testing.T) {
	exit, _, out, _, dbc := startTest(t)

	runCmd(t, exit, "db", "migrate", "-db="+dbc, "pending")
	wantExit(t, exit, out, 0)
	want := "no pending migrations\n"
	if out.String() != want {
		t.Error(out.String())
	}
}
