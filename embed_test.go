package goatcounter_test

import (
	"io/fs"
	"testing"

	. "github.com/marvinrabe/goatcounter"
)

func TestAssets(t *testing.T) {
	assets, err := AssetPaths()
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
		if _, err := fs.Stat(Static, "public/"+built); err != nil {
			t.Errorf("%s: %v", source, err)
		}
	}
}
