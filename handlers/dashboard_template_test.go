package handlers

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/marvinrabe/goatcounter"
)

func TestDashboardTemplateSyntax(t *testing.T) {
	if err := LoadTemplates(os.DirFS("../tpl")); err != nil {
		t.Fatal(err)
	}
}

func TestDashboardTotalsIsReloadableWidget(t *testing.T) {
	if err := LoadTemplates(os.DirFS("../tpl")); err != nil {
		t.Fatal(err)
	}

	html, err := renderTemplate("_dashboard_totals.gohtml", struct {
		Context context.Context
		ID      int
		Loaded  bool
		Err     error
		Group   goatcounter.Group
		Metrics goatcounter.DashboardMetrics
		Series  goatcounter.DashboardMetricSeries
	}{
		Context: context.Background(),
		ID:      7,
		Loaded:  true,
		Group:   goatcounter.GroupDaily,
	})
	if err != nil {
		t.Fatal(err)
	}

	const marker = `<section class="totals dashboard-card widget-loaded" data-widget="7">`
	if !strings.Contains(html, marker) {
		t.Fatalf("totals must expose its loaded widget marker on the reloadable root; output: %s", html)
	}
}
