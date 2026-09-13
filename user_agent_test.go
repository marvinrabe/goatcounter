package goatcounter_test

import (
	"reflect"
	"strings"
	"testing"

	. "github.com/marvinrabe/goatcounter"
	botcheck "github.com/marvinrabe/goatcounter/internal/bot"
	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/testenv"
	"github.com/marvinrabe/goatcounter/internal/testutil"
)

func TestUserAgentGetOrInsert(t *testing.T) {
	ctx := testenv.DB(t)

	test := func(gotUA, wantUA UserAgent, want string) {
		t.Helper()

		if !reflect.DeepEqual(gotUA, wantUA) {
			t.Fatalf("wrong ua\ngot:  %#v\nwant: %#v", gotUA, wantUA)
		}

		want = strings.ReplaceAll(strings.TrimSpace(strings.ReplaceAll(want, "\t", "")), "@", " ")
		out := database.DumpString(ctx, `select browsers.name || ' ' || browsers.version as browser from browsers;`) +
			database.DumpString(ctx, `select systems.name  || ' ' || systems.version  as system  from systems;`)
		out = strings.ReplaceAll(out, " \n", "\n") // database.DumpString pads columns with trailing spaces.
		if d := testutil.Diff(out, want); d != "" {
			t.Error(d)
		}
	}

	{
		ua := UserAgent{UserAgent: "Mozilla/5.0 (X11; Linux x86_64; rv:79.0) Gecko/20100101 Firefox/79.0"}
		err := ua.GetOrInsert(ctx)
		if err != nil {
			t.Fatal(err)
		}
		test(ua, UserAgent{UserAgent: ua.UserAgent, BrowserID: 1, SystemID: 1, Isbot: botcheck.NoBotNoMatch}, `
			browser
			Firefox 79
			system
			Linux
		`)
	}

	{
		ua := UserAgent{UserAgent: "Mozilla/5.0 (X11; Linux x86_64; rv:79.0) Gecko/20100101 Firefox/79.0"}
		err := ua.GetOrInsert(ctx)
		if err != nil {
			t.Fatal(err)
		}
		test(ua, UserAgent{UserAgent: ua.UserAgent, BrowserID: 1, SystemID: 1, Isbot: botcheck.NoBotNoMatch}, `
			browser
			Firefox 79
			system
			Linux
		`)
	}

	{
		ua := UserAgent{UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:79.0) Gecko/20100101 Firefox/79.0"}
		err := ua.GetOrInsert(ctx)
		if err != nil {
			t.Fatal(err)
		}
		test(ua, UserAgent{UserAgent: ua.UserAgent, BrowserID: 1, SystemID: 2, Isbot: botcheck.NoBotNoMatch}, `
			browser
			Firefox 79
			system
			Linux
			Windows 10
		`)
	}

	{
		ua := UserAgent{UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:71.0) Gecko/20100101 Firefox/71.0"}
		err := ua.GetOrInsert(ctx)
		if err != nil {
			t.Fatal(err)
		}
		test(ua, UserAgent{UserAgent: ua.UserAgent, BrowserID: 2, SystemID: 2, Isbot: botcheck.NoBotNoMatch}, `
			browser
			Firefox 79
			Firefox 71
			system
			Linux
			Windows 10
		`)
	}
}
