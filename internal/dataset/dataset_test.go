package dataset

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func parseWords(r io.Reader) ([]string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	words := strings.Fields(string(b))
	if len(words) == 0 {
		return nil, errors.New("empty")
	}
	return words, nil
}

func TestRefresh(t *testing.T) {
	var (
		body     atomic.Value
		status   atomic.Int32
		requests atomic.Int32
	)
	body.Store("a b")
	status.Store(200)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		etag := `"` + body.Load().(string) + `"`
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.WriteHeader(int(status.Load()))
		io.WriteString(w, body.Load().(string))
	}))
	defer srv.Close()

	ctx := t.Context()
	d := New("test", []string{"snapshot"}, srv.URL, parseWords)
	check := func(wantChanged, wantErr bool, want string) {
		t.Helper()
		changed, err := d.Refresh(ctx, srv.Client())
		if changed != wantChanged || (err != nil) != wantErr {
			t.Errorf("changed=%t err=%v; want changed=%t err=%t", changed, err, wantChanged, wantErr)
		}
		if have := strings.Join(d.Load(), " "); have != want {
			t.Errorf("value %q; want %q", have, want)
		}
	}

	if have := strings.Join(d.Load(), " "); have != "snapshot" {
		t.Fatalf("initial value %q", have)
	}
	check(true, false, "a b")
	check(false, false, "a b") // Not modified.

	body.Store("")
	check(false, true, "a b") // Rejected by parse: keep the old.

	body.Store("c")
	status.Store(500)
	check(false, true, "a b")

	status.Store(200)
	check(true, false, "c")

	srv.Close()
	check(false, true, "c") // Network error.
}

func TestRefreshTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, strings.Repeat("x ", MaxSize/2+1))
	}))
	defer srv.Close()

	d := New("test", []string{"snapshot"}, srv.URL, parseWords)
	if _, err := d.Refresh(t.Context(), srv.Client()); !errors.Is(err, errTooLarge) {
		t.Fatalf("err = %v", err)
	}
	if d.Load()[0] != "snapshot" {
		t.Fatal("value replaced")
	}
}

func TestRun(t *testing.T) {
	defer func(f, i, r time.Duration) { FirstDelay, Interval, RetryDelay = f, i, r }(FirstDelay, Interval, RetryDelay)
	FirstDelay, Interval, RetryDelay = time.Millisecond, time.Hour, time.Millisecond

	var fail atomic.Bool
	fail.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Swap(false) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, "updated")
	}))
	defer srv.Close()

	d := New("test", []string{"snapshot"}, srv.URL, parseWords)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { Run(ctx, d); close(done) }()

	// The first try fails, and the retry succeeds.
	deadline := time.Now().Add(5 * time.Second)
	for d.Load()[0] != "updated" {
		if time.Now().After(deadline) {
			t.Fatal("not updated")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
}
