package goatcounter_test

import (
	"context"
	"os"
	"strings"
	"testing"

	. "github.com/marvinrabe/goatcounter"
	_ "github.com/marvinrabe/goatcounter/internal/tpl"
	"zgo.at/ztpl"
)

func TestDashboardTemplateSyntax(t *testing.T) {
	if err := ztpl.Init(os.DirFS("tpl")); err != nil {
		t.Fatal(err)
	}
}

func TestDashboardTotalsIsReloadableWidget(t *testing.T) {
	if err := ztpl.Init(os.DirFS("tpl")); err != nil {
		t.Fatal(err)
	}

	html, err := ztpl.ExecuteString("_dashboard_totals.gohtml", struct {
		Context context.Context
		ID      int
		Loaded  bool
		Err     error
		Group   Group
		Metrics DashboardMetrics
		Series  DashboardMetricSeries
	}{
		Context: context.Background(),
		ID:      7,
		Loaded:  true,
		Group:   GroupDaily,
	})
	if err != nil {
		t.Fatal(err)
	}

	const marker = `<section class="totals dashboard-card widget-loaded" data-widget="7">`
	if !strings.Contains(html, marker) {
		t.Fatalf("totals must expose its loaded widget marker on the reloadable root; output: %s", html)
	}
}
