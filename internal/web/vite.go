package web

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"sync"
)

type viteAsset struct {
	File string `json:"file"`
}

// assetPaths returns Vite's source-to-built-file mapping.
var assetPaths = sync.OnceValues(func() (map[string]string, error) {
	return readAssetPaths(sub(distFiles, "dist"))
})

func readAssetPaths(fsys fs.FS) (map[string]string, error) {
	b, err := fs.ReadFile(fsys, "manifest.json")
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
