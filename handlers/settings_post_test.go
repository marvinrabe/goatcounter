package handlers

import (
	"net/url"
	"strings"
	"testing"
	"zgo.at/zdb"
	"zgo.at/zstd/ztest"

	"github.com/marvinrabe/goatcounter/internal/testenv"
)

func TestSettingsPost(t *testing.T) {
	ctx := testenv.DB(t)

	// Contains the removed allow_embed key: an old form posting a
	// since-removed setting must not abort decoding of the other settings.
	form := url.Values{
		"settings.public":         {"public"},
		"settings.data_retention": {"31"},
		"settings.allow_embed":    {"http://example.com"},
	}
	r, rr := newTest(ctx, "POST", "/settings/main", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	login(t, r)
	newBackend(ctx).ServeHTTP(rr, r)
	ztest.Code(t, rr, 303)

	var rows []struct {
		Settings string `json:"settings"`
	}
	err := zdb.Select(ctx, &rows, "select settings from site")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rows[0].Settings, `"data_retention":31`) {
		t.Errorf("data_retention not saved in: %s", rows[0].Settings)
	}
	if strings.Contains(rows[0].Settings, `"allow_embed"`) {
		t.Errorf("allow_embed leaked into settings: %s", rows[0].Settings)
	}
}
