package main

import (
	"regexp"
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

	runCmd(t, exit, "db", "query", "-db="+dbc, "select user_id, email from users order by user_id")
	wantExit(t, exit, out, 0)

	want := `
		user_id  email
		1        test@testenv.localhost`
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

func grep(s, find string) bool {
	return regexp.MustCompile(find).MatchString(s)
}

func TestDBUser(t *testing.T) {
	exit, _, out, ctx, dbc := startTest(t)

	{ // create
		runCmd(t, exit, "db", "create", "user",
			"-db="+dbc,
			"-email=foo@foo.foo",
			"-password=password")
		wantExit(t, exit, out, 0)

		have := zdb.DumpString(ctx, `select user_id, email from users order by user_id`)
		want := `
			user_id  email
			1        test@testenv.localhost
			2        foo@foo.foo`
		if d := zdb.Diff(have, want); d != "" {
			t.Error(d)
		}
		out.Reset()
	}

	{ // update
		runCmd(t, exit, "db", "update", "user",
			"-db="+dbc,
			"-find=2",
			"-email=new@new.new",
			"-password=password")
		wantExit(t, exit, out, 0)

		have := zdb.DumpString(ctx, `select user_id, email from users order by user_id`)
		want := `
			user_id  email
			1        test@testenv.localhost
			2        new@new.new`
		if d := zdb.Diff(have, want); d != "" {
			t.Error(d)
		}
		out.Reset()
	}

	{ // show
		runCmd(t, exit, "db", "show", "user",
			"-db="+dbc,
			"-find=1", "-find=new@new.new")
		wantExit(t, exit, out, 0)
		if r := `user_id\s+1`; !grep(out.String(), r) {
			t.Errorf("user 1 not found in output (via regexp %q):\n%s", r, out.String())
		}
		if r := `user_id\s+2`; !grep(out.String(), r) {
			t.Errorf("user 2 not found in output (via regexp %q):\n%s", r, out.String())
		}
		out.Reset()
	}

	{ // delete
		runCmd(t, exit, "db", "delete", "user",
			"-db="+dbc,
			"-find=2",
		)
		wantExit(t, exit, out, 0)

		have := zdb.DumpString(ctx, `select user_id, email from users order by user_id`)
		want := `
			user_id  email
			1        test@testenv.localhost`
		if d := zdb.Diff(have, want); d != "" {
			t.Error(d)
		}
		out.Reset()
	}

	{ // delete when it's the last user
		runCmd(t, exit, "db", "delete", "user",
			"-db="+dbc,
			"-find=1",
		)
		wantExit(t, exit, out, 1)

		have := zdb.DumpString(ctx, `select user_id, email from users order by user_id`)
		want := `
			user_id  email
			1        test@testenv.localhost`
		if d := zdb.Diff(have, want); d != "" {
			t.Error(d)
		}
		out.Reset()
	}

	{ // force delete
		runCmd(t, exit, "db", "delete", "user",
			"-db="+dbc,
			"-find=1",
			"-force",
		)
		wantExit(t, exit, out, 0)

		have := zdb.DumpString(ctx, `select user_id, email from users order by user_id`)
		want := `
			user_id  email`
		if d := zdb.Diff(have, want); d != "" {
			t.Error(d)
		}
		out.Reset()
	}
}
