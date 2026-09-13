package geo

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marvinrabe/goatcounter/internal/geo/geoip2"
)

//go:embed GeoLite2-Country.mmdb.gz
var bundle []byte

var ctxkey = &struct{ n string }{"geo"}

func With(ctx context.Context, db *geoip2.Reader) context.Context {
	return context.WithValue(ctx, ctxkey, db)
}

func Get(ctx context.Context) *geoip2.Reader {
	db, ok := ctx.Value(ctxkey).(*geoip2.Reader)
	if !ok {
		return nil
	}
	return db
}

// Open uses the bundled database unless an explicit file path is configured.
// Network updates are a separate one-off operation, never part of startup.
func Open(path string) (*geoip2.Reader, error) {
	if path == "" {
		return openBundled()
	}
	if strings.HasPrefix(path, "maxmind:") {
		return nil, fmt.Errorf("automatic downloads during startup are unsupported; run geodb-update and configure its output path")
	}
	return geoip2.Open(path)
}

// CacheDir contains only disposable copies of the embedded asset.
var CacheDir = filepath.Join(os.TempDir(), "goatcounter-geoip")

// Update downloads a Cities database explicitly, outside the web lifecycle.
func Update(ctx context.Context, accountID, key, path string) error {
	return fetchDB(ctx, accountID, key, path)
}

// openBundled extracts the embedded database to disk (once) and memory-maps it,
// so that the ~10M of data lives in the page cache – where the kernel can
// reclaim it – rather than permanently on the Go heap.
//
// The filename includes a hash of the embedded data, so a GoatCounter build
// with an updated database extracts a fresh copy.
func openBundled() (*geoip2.Reader, error) {
	sum := sha256.Sum256(bundle)
	path := filepath.Join(CacheDir, fmt.Sprintf("bundled-%x.mmdb", sum[:8]))

	if _, err := os.Stat(path); err != nil {
		if err := extractBundle(path); err != nil {
			slog.With("module", "geo").DebugContext(context.Background(), "can't cache the bundled GeoIP database, loading it in memory instead", "error", err)
			return bundleFromMemory()
		}
	}

	db, err := geoip2.Open(path)
	if err != nil {
		// A truncated or corrupted cache file shouldn't be fatal; drop it and
		// carry on from memory. The next start will extract it again.
		slog.With("module", "geo").DebugContext(context.Background(), "can't open the cached GeoIP database, loading it in memory instead", "error", err)
		os.Remove(path)
		return bundleFromMemory()
	}
	return db, nil
}

// extractBundle decompresses the embedded database to path, atomically.
func extractBundle(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	gz, err := gzip.NewReader(bytes.NewReader(bundle))
	if err != nil {
		return err
	}
	defer gz.Close()

	tmp, err := os.CreateTemp(dir, "bundled-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	if _, err := io.Copy(tmp, gz); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// bundleFromMemory is the fallback for when the database can't be cached on
// disk, e.g. a read-only or full filesystem.
func bundleFromMemory() (*geoip2.Reader, error) {
	gz, err := gzip.NewReader(bytes.NewReader(bundle))
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	d, err := io.ReadAll(gz)
	if err != nil {
		return nil, err
	}
	return geoip2.FromBytes(d)
}

func fetch(ctx context.Context, accountID, key, p string) ([]byte, error) {
	var (
		c    = http.Client{Timeout: 10 * time.Second}
		r, _ = http.NewRequestWithContext(ctx, "GET", p, nil)
	)
	r.SetBasicAuth(accountID, key)
	r.Header.Add("User-Agent", "GoatCounter/1.0 (+https://github.com/arp242/goatcounter)")
	resp, err := c.Do(r)
	if err != nil {
		return nil, fmt.Errorf("fetching %q: %w", p, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		if len(b) > 500 {
			b = append(b[:500], []byte("…")...)
		}
		return nil, fmt.Errorf("fetching %q: %s: %s", p, resp.Status, string(b))
	}
	return io.ReadAll(resp.Body)
}

func fetchHash(ctx context.Context, accountID, key string) (string, error) {
	p := "https://download.maxmind.com/geoip/databases/GeoLite2-City/download?suffix=tar.gz.sha256"
	b, err := fetch(ctx, accountID, key, p)
	if err != nil {
		return "", err
	}

	f := strings.Fields(string(b))
	if len(f) != 2 {
		return "", fmt.Errorf("unexpected return for %q: %s", p, string(b))
	}
	return f[0], nil
}

func fetchDB(ctx context.Context, accountID, key, path string) error {
	hash, err := fetchHash(ctx, accountID, key)
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(path), 0o777)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "update-geodb-*")
	if err != nil {
		return err
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	p := "https://download.maxmind.com/geoip/databases/GeoLite2-City/download?suffix=tar.gz"
	b, err := fetch(ctx, accountID, key, p)
	if err != nil {
		return err
	}

	h := sha256.New()
	h.Write(b)
	if hh := fmt.Sprintf("%x", h.Sum(nil)); hh != hash {
		return fmt.Errorf("hash mismatch for %q: have %s, want %s", p, hh, hash)
	}
	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("reading %q: %w", p, err)
	}
	defer gz.Close()
	archive := tar.NewReader(gz)

	for {
		h, err := archive.Next()
		if err != nil {
			// Don't ignore io.EOF, as reaching this means we haven't seen a
			// mmdb file
			return fmt.Errorf("reading %q: %w", p, err)
		}
		if strings.HasSuffix(h.Name, ".mmdb") {
			_, err := io.Copy(tmp, archive)
			if err != nil {
				return fmt.Errorf("writing %q: %w", tmp.Name(), err)
			}
			if err := tmp.Close(); err != nil {
				return fmt.Errorf("writing %q: %w", tmp.Name(), err)
			}
			return os.Rename(tmp.Name(), path)
		}
	}
}
