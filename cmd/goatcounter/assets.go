package main

import (
	"io/fs"
	"os"
)

func embeddedOrDir(embedded fs.FS, path string, dev bool) (fs.FS, error) {
	if dev {
		return os.DirFS(path), nil
	}
	return fs.Sub(embedded, path)
}
