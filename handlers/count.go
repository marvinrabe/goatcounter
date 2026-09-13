package handlers

import (
	"fmt"
	"net/http"

	"github.com/marvinrabe/goatcounter"
	"github.com/monoculum/formam/v3"
	"zgo.at/isbot"
	"zgo.at/zhttp"
	"zgo.at/zstd/ztime"
)

// Use GIF because it's the smallest filesize (PNG is 116 bytes, vs 43 for GIF).
var gif = []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x1, 0x0, 0x1, 0x0, 0x80,
	0x1, 0x0, 0x0, 0x0, 0x0, 0xff, 0xff, 0xff, 0x21, 0xf9, 0x4, 0x1, 0xa, 0x0,
	0x1, 0x0, 0x2c, 0x0, 0x0, 0x0, 0x0, 0x1, 0x0, 0x1, 0x0, 0x0, 0x2, 0x2, 0x4c,
	0x1, 0x0, 0x3b}

func (h backend) count(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "image/gif")
	w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")

	// Note this works in both HTTP/1.1 and HTTP/2, as the Go HTTP/2 server
	// picks up on this and sends the GOAWAY frame.
	// A short idle timeout would be better, but that can't be configured
	// per-handler: https://github.com/golang/go/issues/16100
	w.Header().Set("Connection", "close")

	bot := isbot.Bot(r)
	// Don't track pages fetched with the browser's prefetch algorithm.
	if bot == isbot.BotPrefetch {
		return zhttp.Bytes(w, gif)
	}

	site := Site(r.Context())
	hit := goatcounter.Hit{
		Site:            site.Key,
		UserAgentHeader: r.UserAgent(),
		CreatedAt:       ztime.Now(r.Context()),
		RemoteAddr:      r.RemoteAddr,
	}
	var l goatcounter.Location
	hit.Location = l.LookupIP(r.Context(), r.RemoteAddr)
	hit.Language = goatcounter.AcceptLanguage(r.Header.Get("Accept-Language"))

	err := formam.NewDecoder(&formam.DecoderOptions{
		TagName:           "json",
		IgnoreUnknownKeys: true,
	}).Decode(r.URL.Query(), &hit)
	if err != nil {
		w.Header().Add("X-Goatcounter", fmt.Sprintf("error decoding parameters: %s", err))
		w.WriteHeader(400)
		return zhttp.Bytes(w, gif)
	}
	if hit.Bot > 0 && hit.Bot < 150 {
		w.Header().Add("X-Goatcounter", fmt.Sprintf("wrong value: b=%d", hit.Bot))
		w.WriteHeader(400)
		return zhttp.Bytes(w, gif)
	}
	if len(hit.Path) > 2048 {
		w.Header().Add("X-Goatcounter", fmt.Sprintf("ignored because path is longer than 2048 bytes (%d bytes)", len(r.RequestURI)))
		w.WriteHeader(400)
		return zhttp.Bytes(w, gif)
	}

	if isbot.Is(bot) { // Prefer the backend detection.
		hit.Bot = int(bot)
	}

	err = hit.Validate(r.Context(), true)
	if err != nil {
		w.Header().Add("X-Goatcounter", fmt.Sprintf("not valid: %s", err))
		w.WriteHeader(400)
		return zhttp.Bytes(w, gif)
	}

	if err := goatcounter.EnqueueHits(r.Context(), hit); err != nil {
		return fmt.Errorf("durable collector queue: %w", err)
	}
	return zhttp.Bytes(w, gif)
}
