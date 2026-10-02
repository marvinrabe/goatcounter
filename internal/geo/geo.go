package geo

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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
		return nil, fmt.Errorf("automatic downloads are unsupported; download a GeoIP database and set its path")
	}
	return geoip2.Open(path)
}

// CacheDir contains only disposable copies of the embedded asset.
var CacheDir = filepath.Join(os.TempDir(), "goatcounter-geoip")

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
