package goatcounter

import (
	"net/url"
	"strings"

	"github.com/marvinrabe/goatcounter/internal/refspam"
)

// knownSources maps referrer hosts to the source names Plausible uses. A
// host matches itself and its subdomains. Google, Bing, and Yandex are
// matched on any country domain in sourceName.
var knownSources = map[string]string{
	"linkedin.com": "LinkedIn", "lnkd.in": "LinkedIn", "com.linkedin.android": "LinkedIn",
	"ecosia.org":     "Ecosia",
	"duckduckgo.com": "DuckDuckGo",
	"indeed.com":     "Indeed",
	"t.co":           "X (Twitter)", "twitter.com": "X (Twitter)", "x.com": "X (Twitter)",
	"teams.microsoft.com": "Microsoft Teams", "teams.cdn.office.net": "Microsoft Teams",
	"facebook.com": "Facebook", "fb.com": "Facebook", "fb.me": "Facebook",
	"instagram.com": "Instagram",
	"github.com":    "GitHub",
	"xing.com":      "XING",
	"perplexity.ai": "Perplexity",
	"chatgpt.com":   "ChatGPT", "chat.openai.com": "ChatGPT",
	"search.yahoo.com":     "Yahoo!",
	"baidu.com":            "Baidu",
	"reddit.com":           "Reddit",
	"news.ycombinator.com": "Hacker News",
	"youtube.com":          "Youtube", "youtu.be": "Youtube",
	"wikipedia.org":   "Wikipedia",
	"mail.google.com": "Gmail", "com.google.android.gm": "Gmail",
	"com.google.android.googlequicksearchbox": "Google",
	"outlook.live.com":                        "Outlook.com",
	"brave.com":                               "Brave", "search.brave.com": "Brave",
	"startpage.com": "Startpage",
	"qwant.com":     "Qwant",
}

func sourceName(host string) string {
	for h := host; h != ""; {
		if s, ok := knownSources[h]; ok {
			return s
		}
		_, rest, ok := strings.Cut(h, ".")
		if !ok || !strings.Contains(rest, ".") {
			break
		}
		h = rest
	}
	for _, label := range strings.Split(host, ".") {
		switch label {
		case "google":
			return "Google"
		case "bing":
			return "Bing"
		case "yandex":
			return "Yandex"
		}
	}
	return host
}

// referrerSource derives Plausible's source and referrer dimensions from a
// Referer header. Internal referrals and referrer spam return empty values.
//
// The referrer is the host without "www." plus the path, without query
// string, e.g. "google.com/url" or "news.ycombinator.com/item".
func referrerSource(ref, linkDomain string) (source, referrer string) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return "", ""
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	switch u.Scheme {
	case "http", "https":
	case "android-app":
		// android-app://com.linkedin.android
		return sourceName(host), strings.TrimRight(ref, "/")
	default:
		return "", ""
	}
	if host == "" || refspam.Is(host) {
		return "", ""
	}
	own := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(linkDomain)), "www.")
	if own != "" && (host == own || strings.HasSuffix(host, "."+own)) {
		return "", ""
	}
	referrer = host + strings.TrimRight(u.EscapedPath(), "/")
	return sourceName(host), referrer
}
