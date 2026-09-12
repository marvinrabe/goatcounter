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
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marvinrabe/goatcounter/internal/geo/geoip2"
	"github.com/marvinrabe/goatcounter/internal/log"
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

// Open a geoDB database located at the given path.
//
// The database can be the "Countries" or "Cities" version.
//
// It will use the embeded "Countries" database if path is an empty string.
//
// It will download a database if the path starts with "maxmind:". This needs to
// be as "maxmind:accountID:licenseKey[:path]", where :path is optional and
// detaults to goatcounter-data/auto.mmdb.
func Open(path string) (*geoip2.Reader, error) {
	// Use built-in
	if path == "" {
		return openBundled()
	}

	bundle = nil // Not using it; save some memory.

	// Download update
	if strings.HasPrefix(path, "maxmind:") {
		s := strings.Split(path[8:], ":")
		if l := len(s); l != 2 && l != 3 {
			return nil, fmt.Errorf("invalid format for MaxMind GeoIP update: %q", path)
		}
		accountID, key, dst := s[0], s[1], "goatcounter-data/auto.mmdb"
		if len(s) == 3 {
			dst = s[2]
		}
		st, err := os.Stat(dst)
		if err != nil || st.ModTime().Before(time.Now().Add(-24*time.Hour*7)) {
			log.Module("startup").Info(context.Background(), "downloading GeoDB database; might take a few seconds")
			err = fetchDB(accountID, key, dst)
			if err != nil {
				return nil, err
			}
		}
		path = dst
	}

	// From FS
	return geoip2.Open(path)
}

// CacheDir is where the extracted copy of the embedded database is kept. It's a
// subdirectory so that the "any .mmdb in goatcounter-data" detection in the
// serve command doesn't mistake it for a database the user supplied.
//
// It's relative, so it resolves against the working directory. Tests should
// point it somewhere outside the source tree; see internal/testenv.
var CacheDir = filepath.Join("goatcounter-data", "cache")

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
			log.Module("geo").Debugf(context.Background(),
				"can't cache the bundled GeoIP database, loading it in memory instead: %s", err)
			return bundleFromMemory()
		}
	}

	db, err := geoip2.Open(path)
	if err != nil {
		// A truncated or corrupted cache file shouldn't be fatal; drop it and
		// carry on from memory. The next start will extract it again.
		log.Module("geo").Debugf(context.Background(),
			"can't open the cached GeoIP database, loading it in memory instead: %s", err)
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

	// Clean up copies extracted by older builds.
	old, _ := filepath.Glob(filepath.Join(dir, "bundled-*.mmdb"))
	for _, o := range old {
		if o != path {
			os.Remove(o)
		}
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

func fetch(accountID, key, p string) ([]byte, error) {
	var (
		c    = http.Client{Timeout: 10 * time.Second}
		r, _ = http.NewRequest("GET", p, nil)
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

func fetchHash(accountID, key string) (string, error) {
	p := "https://download.maxmind.com/geoip/databases/GeoLite2-City/download?suffix=tar.gz.sha256"
	b, err := fetch(accountID, key, p)
	if err != nil {
		return "", err
	}

	f := strings.Fields(string(b))
	if len(f) != 2 {
		return "", fmt.Errorf("unexpected return for %q: %s", p, string(b))
	}
	return f[0], nil
}

func fetchDB(accountID, key, path string) error {
	hash, err := fetchHash(accountID, key)
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
	b, err := fetch(accountID, key, p)
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
