package goatcounter

import "embed"

// Schema creates the database tables.
//
//go:embed db/schema.sql
var Schema string

// Static contains all the static files to serve.
//
//go:embed public/*
var Static embed.FS

// Templates contains all templates.
//
//go:embed tpl/*
var Templates embed.FS
