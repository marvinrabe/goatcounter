package goatcounter

import (
	"embed"
	"html/template"

	"zgo.at/zdb"
)

// DB contains all files in db/*
//
//go:embed db/schema.gotxt
//go:embed db/languages.sql
//go:embed db/query/*
var DB embed.FS

// Static contains all the static files to serve.
//
//go:embed public/*
var Static embed.FS

// Templates contains all templates.
//
//go:embed tpl/*
var Templates embed.FS

func init() {
	zdb.TemplateFuncMap = template.FuncMap{
		// Include another file from db/ in the schema or a migration; the list
		// of languages is needed by both and is too large to keep two copies
		// of. Always read from the embedded files, also with -dev: this is
		// static data that only changes when the binary is rebuilt.
		"include": func(path string) (string, error) {
			b, err := DB.ReadFile("db/" + path)
			return string(b), err
		},
	}
}
