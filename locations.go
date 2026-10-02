package goatcounter

import (
	"context"
	"log/slog"
	"net"
	"sync"

	"github.com/marvinrabe/goatcounter/internal/geo"
)

// GeoLocation is the location of a pageview, in Plausible's format.
type GeoLocation struct {
	Country string // ISO 3166-1 alpha-2: "DE".
	Region  string // ISO 3166-2: "DE-HE".
	City    int    // GeoNames ID.
}

// LookupIP finds the location for an IP address in the GeoIP database. It
// returns an empty location if it's unknown.
func LookupIP(ctx context.Context, ip string) GeoLocation {
	geodb := geo.Get(ctx)
	if geodb == nil {
		return GeoLocation{}
	}
	loc, err := geodb.City(net.ParseIP(ip))
	if err != nil {
		return GeoLocation{}
	}
	l := GeoLocation{Country: loc.Country.IsoCode, City: int(loc.City.GeoNameID)}
	if l.Country != "" && len(loc.Subdivisions) > 0 && loc.Subdivisions[0].IsoCode != "" {
		l.Region = l.Country + "-" + loc.Subdivisions[0].IsoCode
	}
	return l
}

var geoNames struct {
	once sync.Once
	m    map[string]string
}

// GeoName gets the English name for a country ("DE") or region ("DE-HE")
// code. It returns an empty string if the GeoIP database doesn't know it.
//
// The names are read from the GeoIP database once, on first use.
func GeoName(ctx context.Context, code string) string {
	geoNames.once.Do(func() {
		geoNames.m = make(map[string]string, 300)
		geodb := geo.Get(ctx)
		if geodb == nil {
			return
		}
		iter := geodb.DB().Data()
		for iter.Next() {
			var r struct {
				Country struct {
					ISOCode string            `maxminddb:"iso_code"`
					Names   map[string]string `maxminddb:"names"`
				} `maxminddb:"country"`
				Subdivisions []struct {
					ISOCode string            `maxminddb:"iso_code"`
					Names   map[string]string `maxminddb:"names"`
				} `maxminddb:"subdivisions"`
			}
			if err := iter.Data(&r); err != nil {
				slog.ErrorContext(ctx, "decode GeoIP record", "error", err)
				return
			}
			if r.Country.ISOCode == "" {
				continue
			}
			geoNames.m[r.Country.ISOCode] = r.Country.Names["en"]
			if len(r.Subdivisions) > 0 && r.Subdivisions[0].ISOCode != "" {
				geoNames.m[r.Country.ISOCode+"-"+r.Subdivisions[0].ISOCode] = r.Subdivisions[0].Names["en"]
			}
		}
	})
	return geoNames.m[code]
}
