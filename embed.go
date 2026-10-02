package goatcounter

import "embed"

// DB contains the database schema.
//
//go:embed db/schema.gotxt
var DB embed.FS

// Static contains all the static files to serve.
//
//go:embed public/*
var Static embed.FS

// Templates contains all templates.
//
//go:embed tpl/*
var Templates embed.FS
