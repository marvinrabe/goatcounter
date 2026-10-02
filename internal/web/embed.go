// Package web serves the collector, the dashboard, and their assets over HTTP.
// The templates and the Vite-built frontend are embedded.
package web

import (
	"embed"
	"io/fs"
)

var (
	//go:embed templates/*
	templateFiles embed.FS
	// Built by Vite from assets/; run "npm run build" first.
	//
	//go:embed dist/*
	distFiles embed.FS
)

func sub(fsys fs.FS, dir string) fs.FS {
	s, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return s
}
