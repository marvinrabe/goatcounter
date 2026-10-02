package enrich

// Request headers set by the CDN in front, bunny.net. GoatCounter trusts them,
// so all traffic must go through the CDN; switching CDNs means changing these.
const (
	// IPHeader has the visitor's IP address.
	IPHeader = "X-Real-IP"
	// CountryHeader has the visitor's ISO 3166-1 alpha-2 country code.
	CountryHeader = "CDN-RequestCountryCode"
	// ProtoHeader is "https" when the visitor connected with TLS.
	ProtoHeader = "X-Forwarded-Proto"
)
