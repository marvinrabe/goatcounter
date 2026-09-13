package goatcounter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/marvinrabe/goatcounter/internal/database"
	"github.com/marvinrabe/goatcounter/internal/geo"
)

type LocationID int32

type Location struct {
	ID LocationID `db:"location_id,id" json:"-"`

	Country     string `db:"country" json:"country"`
	Region      string `db:"region" json:"region"`
	CountryName string `db:"country_name" json:"country_name"`
	RegionName  string `db:"region_name" json:"region_name"`

	// staticcheck flags this even though "ISO" is an initialism.
	ISO3166_2 string `db:"iso_3166_2,noinsert" json:"-"` //lint:ignore ST1003 staticcheck bug
}

func (Location) Table() string { return "locations" }

func (l Location) String() string {
	return fmt.Sprintf("location_id=%d; country=%q; country_name=%q; region=%q; region_name=%q", l.ID, l.Country, l.CountryName, l.Region, l.RegionName)
}

// ByCode gets a location by ISO-3166-2 code; e.g. "US" or "US-TX".
func (l *Location) ByCode(ctx context.Context, code string) error {
	if ll, ok := cachedLocation(ctx, code); ok {
		*l = ll
		return nil
	}

	err := database.Get(ctx, l, `select * from locations where iso_3166_2 = ?`, code)
	if database.ErrNoRows(err) {
		l.ISO3166_2 = code
		l.Country, l.Region, _ = strings.Cut(code, "-")
		l.CountryName, l.RegionName = findGeoName(ctx, l.Country, l.Region)
		err = l.insert(ctx)
	}
	if err != nil {
		return fmt.Errorf("Location.ByCode: %w", err)
	}

	l.cache(ctx)
	return nil
}

// Lookup a location by IPv4 or IPv6 address.
//
// This will insert a row in the locations table if one doesn't exist yet.
func (l *Location) Lookup(ctx context.Context, ip string) error {
	geodb := geo.Get(ctx)
	if geodb == nil {
		return errors.New("Location.Lookup: no geodb on context")
	}

	loc, err := geodb.City(net.ParseIP(ip))
	if err != nil {
		return fmt.Errorf("Location.Lookup: %w", err)
	}
	l.Country = loc.Country.IsoCode
	l.CountryName = loc.Country.Names["en"]
	if len(loc.Subdivisions) > 0 {
		l.Region, l.RegionName = loc.Subdivisions[0].IsoCode, loc.Subdivisions[0].Names["en"]
	}

	l.ISO3166_2 = loc.Country.IsoCode
	if l.Region != "" {
		l.ISO3166_2 += "-" + l.Region
	}
	if ll, ok := cachedLocation(ctx, l.ISO3166_2); ok {
		*l = ll
		return nil
	}

	err = database.Get(ctx, l,
		`select * from locations where country = ? and region = ?`,
		l.Country, l.Region)
	if database.ErrNoRows(err) {
		err = l.insert(ctx)
	}
	if err != nil {
		return fmt.Errorf("Location.Lookup: %w", err)
	}

	l.cache(ctx)
	return nil
}

// LookupIP is a shorthand for Lookup(); returns id 1 on errors ("unknown").
func (l Location) LookupIP(ctx context.Context, ip string) string {
	err := l.Lookup(ctx, ip)
	if err != nil {
		return "" // Special ID: "unknown".
	}
	return l.ISO3166_2
}

func (l *Location) insert(ctx context.Context) (err error) {
	err = database.Insert(ctx, l)
	if err != nil {
		return err
	}

	// Make sure there is an entry for the country as well.
	if l.Region != "" {
		err := (&Location{}).ByCode(ctx, l.Country)
		if err != nil {
			return err
		}
	}
	return nil
}

// This takes ~13s for a full iteration for the Cities database on my laptop
// (Countries is much faster, ~100ms) which is not a great worst case scenario,
// but in most cases it should be (much) faster, and this should get called
// extremely infrequently anyway, if ever.
func findGeoName(ctx context.Context, country, region string) (string, string) {
	geodb := geo.Get(ctx)
	if geodb == nil {
		panic("Location.Lookup: ")
	}

	hasRegions := geodb.Metadata().DatabaseType == "City"
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
		err := iter.Data(&r)
		if err != nil {
			slog.ErrorContext(context.Background(), err.Error())
			return "", ""
		}

		switch {
		// Country database, no region.
		case r.Country.ISOCode == country && !hasRegions:
			return r.Country.Names["en"], ""
		// City database, no region requested.
		case r.Country.ISOCode == country && region == "":
			return r.Country.Names["en"], ""
		// Match region.
		case r.Country.ISOCode == country && len(r.Subdivisions) > 0 && r.Subdivisions[0].ISOCode == region:
			return r.Country.Names["en"], r.Subdivisions[0].Names["en"]
		}
	}
	return "", ""
}
