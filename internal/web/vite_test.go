package web

import (
	"io/fs"
	"testing"
)

func TestAssets(t *testing.T) {
	assets, err := assetPaths()
	if err != nil {
		t.Fatal(err)
	}

	for _, source := range []string{
		"assets/css/backend.css",
		"assets/js/backend.js",
		"assets/js/count.js",
	} {
		built, ok := assets[source]
		if !ok {
			t.Errorf("%s is missing from Vite manifest", source)
			continue
		}
		if _, err := fs.Stat(sub(distFiles, "dist"), built); err != nil {
			t.Errorf("%s: %v", source, err)
		}
	}
}
