package goatcounter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"strconv"
	"sync"
	"time"
)

// Hash of everything in public/; computed once, on first use.
var staticHash = sync.OnceValue(func() string {
	h := sha256.New()
	err := fs.WalkDir(Static, "public", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := Static.ReadFile(p)
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%x\x00", p, sha256.Sum256(b))
		return nil
	})
	if err != nil {
		// Fall back to something that always busts rather than serving stale
		// assets forever.
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
})

// StaticVersion is the cache-busting value appended to static assets as "?v=".
//
// It's a hash of the assets' contents, so it changes exactly when they do:
// unlike the build version it doesn't stay the same across rebuilds from a
// dirty working tree, and unlike a timestamp it doesn't change when nothing did.
func StaticVersion(ctx context.Context) string {
	if Config(ctx).Dev {
		// Read from the filesystem and can change while the server is running.
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return staticHash()
}
