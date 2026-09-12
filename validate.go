package goatcounter

import (
	"context"

	"github.com/marvinrabe/goatcounter/internal/i18n"
	"zgo.at/zvalidate"
)

func NewValidate(ctx context.Context) zvalidate.Validator {
	v := zvalidate.New()
	v.Messages(zvalidate.Messages{
		Required:    func() string { return i18n.T(ctx, "validate/required|must be set") },
		Domain:      func() string { return i18n.T(ctx, "validate/domain|must be a valid domain") },
		Hostname:    func() string { return i18n.T(ctx, "validate/hostname|must be a valid hostname") },
		URL:         func() string { return i18n.T(ctx, "validate/url|must be a valid url") },
		Email:       func() string { return i18n.T(ctx, "validate/email|must be a valid email address") },
		IPv4:        func() string { return i18n.T(ctx, "validate/ipv4|must be a valid IPv4 address") },
		IP:          func() string { return i18n.T(ctx, "validate/ip|must be a valid IPv4 or IPv6 address") },
		HexColor:    func() string { return i18n.T(ctx, "validate/color|must be a valid color code") },
		LenLonger:   func() string { return i18n.T(ctx, "validate/len-longer|must be longer than %d characters") },
		LenShorter:  func() string { return i18n.T(ctx, "validate/len-shorter|must be shorter than %d characters") },
		Exclude:     func() string { return i18n.T(ctx, "validate/exclude|cannot be ‘%s’") },
		Include:     func() string { return i18n.T(ctx, "validate/include|must be one of ‘%s’") },
		Integer:     func() string { return i18n.T(ctx, "validate/int|must be a whole number") },
		Bool:        func() string { return i18n.T(ctx, "validate/bool|must be a boolean") },
		Date:        func() string { return i18n.T(ctx, "validate/date|must be a date as ‘%s’") },
		Phone:       func() string { return i18n.T(ctx, "validate/phone|must be a valid phone number") },
		RangeHigher: func() string { return i18n.T(ctx, "validate/range-higher|must be %d or higher") },
		RangeLower:  func() string { return i18n.T(ctx, "validate/range-lower|must be %d or lower") },
		UTF8:        func() string { return i18n.T(ctx, "validate/utf8|must be UTF-8") },
		Contains:    func() string { return i18n.T(ctx, "validate/contains|cannot contain the characters %s") },
	})
	return v
}
