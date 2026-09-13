package handlers

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marvinrabe/goatcounter/internal/widgets"
)

type lifecycleWidget struct {
	widgets.TotalCount
	getData func(context.Context) error
}

func (w *lifecycleWidget) GetData(ctx context.Context, _ widgets.Args) (bool, error) {
	return false, w.getData(ctx)
}

func TestDashboardStopsQueriesWithRequest(t *testing.T) {
	for _, reason := range []string{"cancelled", "deadline"} {
		t.Run(reason, func(t *testing.T) {
			ctx := context.Background()
			var cancel context.CancelFunc
			if reason == "deadline" {
				ctx, cancel = context.WithTimeout(ctx, 500*time.Millisecond)
			} else {
				ctx, cancel = context.WithCancel(ctx)
			}
			defer cancel()
			started := make(chan struct{}, 2)
			release := make(chan struct{})
			defer close(release)
			var active atomic.Int32
			query := func(ctx context.Context) error {
				active.Add(1)
				defer active.Add(-1)
				started <- struct{}{}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-release:
					return context.Canceled
				}
			}
			list := widgets.List{&lifecycleWidget{getData: query}, &lifecycleWidget{getData: query}}
			r := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
			done := make(chan error, 1)
			go func() {
				done <- (backend{dashTimeout: 10}).loadDashboardWidgets(r, list, widgets.Args{})
			}()
			// Both queries must start before either completes.
			for range list {
				select {
				case <-started:
				case <-time.After(2 * time.Second):
					t.Fatal("dashboard did not start concurrent queries")
				}
			}
			if reason == "cancelled" {
				cancel()
			}
			select {
			case err := <-done:
				if err == nil || !errors.Is(err, ctx.Err()) {
					t.Errorf("dashboard returned %v; want %v", err, ctx.Err())
				}
			case <-time.After(2 * time.Second):
				t.Fatal("dashboard queries ignored request cancellation")
			}
			if got := active.Load(); got != 0 {
				t.Errorf("dashboard returned with %d queries still running", got)
			}
		})
	}
}

func TestDashboardWidgetFailureIsIsolated(t *testing.T) {
	for _, failure := range []string{"error", "panic"} {
		t.Run(failure, func(t *testing.T) {
			bad := &lifecycleWidget{getData: func(context.Context) error {
				if failure == "panic" {
					panic("private query detail")
				}
				return errors.New("private query detail")
			}}
			loaded := false
			good := &lifecycleWidget{getData: func(context.Context) error {
				loaded = true
				return nil
			}}
			r := httptest.NewRequest("GET", "/", nil)
			err := (backend{dashTimeout: 10}).loadDashboardWidgets(r, widgets.List{bad, good}, widgets.Args{})
			if err != nil || !loaded || good.Err() != nil {
				t.Errorf("widget failure affected other widgets: err=%v, loaded=%v, widget err=%v", err, loaded, good.Err())
			}
			if bad.Err() == nil {
				t.Fatal("failed widget has no error")
			}
			if strings.Contains(bad.Err().Error(), "private query detail") {
				t.Errorf("widget exposed internal error: %v", bad.Err())
			}
		})
	}
}
