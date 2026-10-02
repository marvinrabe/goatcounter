package goatcounter

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"sync"
)

type viteAsset struct {
	File string `json:"file"`
}

var embeddedAssetPaths = sync.OnceValues(func() (map[string]string, error) {
	return readAssetPaths(Static)
})

// AssetPaths returns Vite's source-to-built-file mapping.
func AssetPaths() (map[string]string, error) {
	return embeddedAssetPaths()
}

func readAssetPaths(fsys fs.FS) (map[string]string, error) {
	b, err := fs.ReadFile(fsys, "public/manifest.json")
	if err != nil {
		return nil, fmt.Errorf("read Vite manifest: %w", err)
	}

	var manifest map[string]viteAsset
	if err := json.Unmarshal(b, &manifest); err != nil {
		return nil, fmt.Errorf("parse Vite manifest: %w", err)
	}

	paths := make(map[string]string, len(manifest))
	for source, asset := range manifest {
		if asset.File == "" {
			return nil, fmt.Errorf("vite manifest entry %q has no file", source)
		}
		paths[source] = asset.File
	}
	return paths, nil
}
