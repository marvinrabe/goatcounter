package main

import (
	"context"
	"fmt"
	"time"

	"github.com/marvinrabe/goatcounter/internal/geo"
)

const usageGeoDB = `Download a GeoIP Cities database as an explicit one-off operation.

Flags (also read from GOATCOUNTER_* environment variables):
  -maxmind-account-id  MaxMind account ID.
  -maxmind-license     MaxMind license key.
  -geodb              Output .mmdb path; required.

Mount the resulting file read-only and set GOATCOUNTER_GEODB when serving.
`

func cmdGeoDB(args []string, ready chan<- struct{}, stop chan struct{}) error {
	f := newFlags("cmdGeoDB")
	defer func() { ready <- struct{}{} }()
	account := f.String("maxmind-account-id", "", "")
	license := f.String("maxmind-license", "", "")
	path := f.String("geodb", "", "")
	if err := parseFlags(f, args, true, true); err != nil {
		return err
	}
	if *account == "" || *license == "" || *path == "" {
		return fmt.Errorf("-maxmind-account-id, -maxmind-license and -geodb are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return geo.Update(ctx, *account, *license, *path)
}
