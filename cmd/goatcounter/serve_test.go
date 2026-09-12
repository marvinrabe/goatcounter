package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"zgo.at/zvalidate"
)

func TestSetupRatelimits(t *testing.T) {
	for _, tt := range []struct {
		spec     string
		tokens   uint64
		interval time.Duration
		wantErr  string
	}{
		{"", 4, time.Second, ""},
		{"count:2/60", 2, time.Minute, ""},
		{" COUNT : 8 / 1 ", 8, time.Second, ""},
		{"count:none", 0, 0, ""},
		{"count:2/1,count:none", 0, 0, ""},
		{"count:none,count:3/60", 3, time.Minute, ""},
		{"api:4/1", 0, 0, "only count is supported"},
		{"api:none", 0, 0, "only count is supported"},
		{"api2:500/3600", 0, 0, "only count is supported"},
		{"apicount:none", 0, 0, "only count is supported"},
		{"api-count:60/120", 0, 0, "only count is supported"},
		{"export:1/3600", 0, 0, "only count is supported"},
		{"login:20/60", 0, 0, "only count is supported"},
		{"missing:none", 0, 0, "only count is supported"},
		{"count:0/1", 0, 0, "-ratelimit.requests"},
		{"count:-1/1", 0, 0, "-ratelimit.requests"},
		{"count:nope/1", 0, 0, "-ratelimit.requests"},
		{"count:1/0", 0, 0, "-ratelimit.seconds"},
		{"count:1/-1", 0, 0, "-ratelimit.seconds"},
		{"count:1/9223372037", 0, 0, "-ratelimit.seconds"},
		{"count:1", 0, 0, "-ratelimit.seconds"},
		{"count", 0, 0, "-ratelimit.requests"},
	} {
		t.Run(tt.spec, func(t *testing.T) {
			v := zvalidate.New()
			limits := setupRatelimits(&v, tt.spec)
			t.Cleanup(limits.ClearCount)
			if tt.wantErr != "" {
				if !v.HasErrors() || !strings.Contains(v.Error(), tt.wantErr) {
					t.Fatalf("error = %v; want %q", v, tt.wantErr)
				}
				return
			}
			if v.HasErrors() {
				t.Fatal(v)
			}
			if tt.tokens == 0 {
				if limits.Count != nil {
					t.Fatal("collector limit should be disabled")
				}
				return
			}
			tokens, _, reset, ok, err := limits.Count.Take(context.Background(), "visitor")
			if err != nil || !ok || tokens != tt.tokens {
				t.Fatalf("tokens=%d ok=%t error=%v; want %d", tokens, ok, err, tt.tokens)
			}
			remaining := time.Until(time.Unix(0, int64(reset)))
			if remaining < tt.interval-time.Second || remaining > tt.interval {
				t.Errorf("interval = %s; want about %s", remaining, tt.interval)
			}
		})
	}
}

func TestServe(t *testing.T) {
	exit, _, _, _, dbc := startTest(t)

	ready := make(chan struct{}, 1)
	stop := make(chan struct{})
	go runCmdStop(t, exit, ready, stop, "serve",
		"-db="+dbc,
		"-debug=all",
		"-listen=localhost:31874")
	<-ready

	resp, err := http.Get("http://localhost:31874/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Errorf("status %d: %s", resp.StatusCode, b)
	}
	if strings.TrimSpace(string(b)) != "OK" {
		t.Errorf("body: %q", b)
	}

	stop <- struct{}{}
	mainDone.Wait()
}
