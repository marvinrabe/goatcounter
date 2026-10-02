package enrich

import (
	"strings"

	"github.com/medama-io/go-useragent"
	"github.com/medama-io/go-useragent/agents"
)

var uaParser = useragent.NewParser()

// Browser and OS names as Plausible reports them, so imported and collected
// rows group together.
var (
	browserNames = map[agents.Browser]string{
		agents.BrowserEdge:      "Microsoft Edge",
		agents.BrowserIE:        "Internet Explorer",
		agents.BrowserOperaMini: "Opera",
	}
	osNames = map[agents.OS]string{
		agents.OSMacOS:    "Mac",
		agents.OSLinux:    "GNU/Linux",
		agents.OSChromeOS: "Chrome OS",
	}
)

// Device names as Plausible reports them, except that Plausible's laptops are
// Desktop: Plausible tells them apart by the screen width, which isn't
// collected. Plausible has no TVs; they're only in collected rows.
const (
	DeviceMobile  = "Mobile"
	DeviceTablet  = "Tablet"
	DeviceDesktop = "Desktop"
	DeviceTV      = "TV"
)

// UserAgent is the browser, OS, and device dimensions.
type UserAgent struct{ Browser, BrowserVersion, OS, Device string }

// ParseUserAgent gets the browser, OS, and device from a User-Agent header,
// with the browser version truncated to major.minor as in Plausible exports
// ("Chrome 120.0"). The device is empty if it's unknown.
func ParseUserAgent(raw string) UserAgent {
	ua := uaParser.Parse(raw)
	r := UserAgent{
		Browser:        string(ua.Browser()),
		BrowserVersion: majorMinor(ua.BrowserVersion()),
		OS:             string(ua.OS()),
	}
	if n, ok := browserNames[ua.Browser()]; ok {
		r.Browser = n
	}
	if n, ok := osNames[ua.OS()]; ok {
		r.OS = n
	}

	switch ua.Device() {
	case agents.DeviceMobile:
		r.Device = DeviceMobile
		// Android tablets leave out "Mobile"; the parser counts them as phones.
		if ua.OS() == agents.OSAndroid && !strings.Contains(raw, "Mobile") {
			r.Device = DeviceTablet
		}
	case agents.DeviceTablet:
		r.Device = DeviceTablet
	case agents.DeviceDesktop:
		// iPads with Safari in desktop mode, the default, look like a Mac.
		r.Device = DeviceDesktop
	case agents.DeviceTV:
		r.Device = DeviceTV
	}

	switch ua.OS() {
	case agents.OSLinux:
		// Plausible reports common distributions as their own OS.
		for _, distro := range []string{"Ubuntu", "Fedora", "Debian"} {
			if strings.Contains(raw, distro) {
				r.OS = distro
				break
			}
		}
	case agents.OSIOS:
		if strings.Contains(raw, "iPad") {
			r.OS = "iPadOS"
		}
	}
	return r
}

// majorMinor truncates a version to major.minor: "120.0.6099.71" is "120.0",
// and "17" is "17.0".
func majorMinor(v string) string {
	if v == "" {
		return ""
	}
	parts := strings.SplitN(v, ".", 3)
	if len(parts) == 1 {
		parts = append(parts, "0")
	}
	return parts[0] + "." + parts[1]
}
