package enrich

// A dataset keeps lookup data from an upstream list fresh in a long-running
// process.
//
// Every dataset starts from a snapshot that is compiled in, so startup never
// touches the network. Update then downloads the upstream list now and then and
// swaps it in; if that fails for whatever reason, the data in use is kept and
// the download is retried later.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// dataset is a value that is replaced, as a whole, by a newer upstream
// version. It is safe for concurrent use; Load never blocks.
type dataset[T any] struct {
	name  string
	url   string
	parse func(io.Reader) (T, error)
	v     atomic.Pointer[T]

	mu            sync.Mutex // Serializes Refresh.
	etag, lastMod string
}

// newDataset creates a dataset that holds initial until a download of url has been
// parsed successfully. parse must reject incomplete data, as it replaces the
// current value as a whole.
func newDataset[T any](name string, initial T, url string, parse func(io.Reader) (T, error)) *dataset[T] {
	d := &dataset[T]{name: name, url: url, parse: parse}
	d.v.Store(&initial)
	return d
}

// Load gets the current value.
func (d *dataset[T]) Load() T { return *d.v.Load() }

// updatable reports whether there is an upstream to refresh from. Datasets
// without one only have their initial value.
func (d *dataset[T]) updatable() bool { return d.url != "" }

// Name of the dataset, for logs.
func (d *dataset[T]) Name() string { return d.name }

// maxSize is the largest download accepted, so that a broken upstream can't
// use up the memory.
const maxSize = 16 << 20

// Refresh downloads the upstream list and replaces the value if it changed.
// The download is conditional, so an unchanged list costs a request and no
// parsing.
func (d *dataset[T]) Refresh(ctx context.Context, client *http.Client) (changed bool, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "GoatCounter (dataset update)")
	if d.etag != "" {
		req.Header.Set("If-None-Match", d.etag)
	}
	if d.lastMod != "" {
		req.Header.Set("If-Modified-Since", d.lastMod)
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotModified:
		return false, nil
	default:
		return false, fmt.Errorf("%s: %s", d.url, resp.Status)
	}

	v, err := d.parse(&limitReader{r: resp.Body, n: maxSize})
	if err != nil {
		return false, fmt.Errorf("%s: %w", d.url, err)
	}
	d.v.Store(&v)
	d.etag, d.lastMod = resp.Header.Get("ETag"), resp.Header.Get("Last-Modified")
	return true, nil
}

// refresher is a dataset as run sees it.
type refresher interface {
	Name() string
	Refresh(context.Context, *http.Client) (bool, error)
}

// Schedule for run. Datasets are refreshed one at a time, so only one download
// is in memory at once.
var (
	firstDelay  = 10 * time.Minute // First refresh: random, up to this.
	interval    = 24 * time.Hour   // Then: this, plus up to 10% at random.
	retryDelay  = 5 * time.Minute  // After an error: doubled for every error,
	maxRetry    = 6 * time.Hour    // to at most this.
	httpTimeout = time.Minute
)

// run refreshes the datasets until ctx is cancelled. Failures are logged and
// retried with backoff; the data in use stays as it was.
func run(ctx context.Context, sets ...refresher) {
	if len(sets) == 0 {
		return
	}
	// The connections are closed after each refresh: there's no use in
	// keeping them around for a day.
	client := &http.Client{Timeout: httpTimeout, Transport: &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		TLSHandshakeTimeout: 10 * time.Second,
		DisableKeepAlives:   true,
	}}
	l := slog.With("module", "dataset")

	type state struct {
		next  time.Time
		fails int
	}
	now := time.Now()
	states := make([]state, len(sets))
	for i := range states {
		states[i].next = now.Add(jitter(firstDelay))
	}

	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		// Wait for the dataset that is due first.
		due := 0
		for i := range states {
			if states[i].next.Before(states[due].next) {
				due = i
			}
		}
		timer.Reset(time.Until(states[due].next))
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		s, st := sets[due], &states[due]
		changed, err := s.Refresh(ctx, client)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			delay := min(retryDelay<<st.fails, maxRetry)
			st.fails = min(st.fails+1, 16)
			st.next = time.Now().Add(delay + jitter(delay/10))
			l.WarnContext(ctx, "dataset update failed; keeping the current data",
				"dataset", s.Name(), "retry_in", delay.Round(time.Second).String(), "error", err)
		default:
			st.fails = 0
			st.next = time.Now().Add(interval + jitter(interval/10))
			if changed {
				l.InfoContext(ctx, "dataset updated", "dataset", s.Name())
			} else {
				l.DebugContext(ctx, "dataset unchanged", "dataset", s.Name())
			}
		}
	}
}

func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return rand.N(d)
}

// limitReader is io.LimitReader, but it fails rather than cutting off the
// data at the limit.
type limitReader struct {
	r io.Reader
	n int64
}

var errTooLarge = errors.New("download is too large")

func (l *limitReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		// See if there's more, as a list of exactly maxSize is fine.
		var b [1]byte
		if n, _ := l.r.Read(b[:]); n > 0 {
			return 0, errTooLarge
		}
		return 0, io.EOF
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= int64(n)
	return n, err
}
