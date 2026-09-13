package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/marvinrabe/goatcounter/internal/geo"
	"zgo.at/zli"
)

const usageGeoDB = `Download a GeoIP Cities database as an explicit one-off operation.

Flags (also read from GOATCOUNTER_* environment variables):
  -maxmind-account-id  MaxMind account ID.
  -maxmind-license     MaxMind license key.
  -geodb              Output .mmdb path; required.

Mount the resulting file read-only and set GOATCOUNTER_GEODB when serving.
`

func cmdGeoDB(f zli.Flags, ready chan<- struct{}, stop chan struct{}) error {
	defer func() { ready <- struct{}{} }()
	account := f.String("", "maxmind-account-id")
	license := f.String("", "maxmind-license")
	path := f.String("", "geodb")
	if err := f.Parse(zli.FromEnv("GOATCOUNTER")); err != nil {
		// Other application settings may be present for this one-off command.
		var unknown zli.ErrUnknownEnv
		if !errors.As(err, &unknown) {
			return err
		}
	}
	if account.String() == "" || license.String() == "" || path.String() == "" {
		return fmt.Errorf("-maxmind-account-id, -maxmind-license and -geodb are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return geo.Update(ctx, account.String(), license.String(), path.String())
}
