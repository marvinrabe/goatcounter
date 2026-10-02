package enrich

import (
	"strings"

	"github.com/mileusna/useragent"
)

// Browser and OS names as Plausible reports them, so imported and collected
// rows group together.
var (
	browserNames = map[string]string{
		useragent.Edge:           "Microsoft Edge",
		useragent.MobileSafari:   "Safari",
		useragent.OperaMini:      "Opera",
		useragent.OperaTouch:     "Opera",
		useragent.HeadlessChrome: "Chrome",
		useragent.Msie:           "Internet Explorer",
		useragent.FacebookApp:    "Mobile App",
		useragent.InstagramApp:   "Mobile App",
		useragent.TiktokApp:      "Mobile App",
	}
	osNames = map[string]string{
		useragent.MacOS:     "Mac",
		useragent.Linux:     "GNU/Linux",
		useragent.ChromeOS:  "Chrome OS",
		useragent.CrOS:      "Chrome OS",
		useragent.WindowsNT: "Windows",
	}
)

// Device names as Plausible reports them. Plausible tells laptops from
// desktops by the screen width, which isn't collected; both are Desktop.
const (
	DeviceMobile  = "Mobile"
	DeviceTablet  = "Tablet"
	DeviceLaptop  = "Laptop" // Only in rows migrated from Plausible.
	DeviceDesktop = "Desktop"
)

// UserAgent is the browser, OS, and device dimensions.
type UserAgent struct{ Browser, BrowserVersion, OS, OSVersion, Device string }

// ParseUserAgent gets the browser, OS, and device from a User-Agent header,
// with versions truncated to major.minor as in Plausible exports ("Chrome
// 120.0", "Mac 10.15"). The device is empty if it's unknown.
func ParseUserAgent(raw string) UserAgent {
	ua := useragent.Parse(raw)
	version := func(s string) string {
		if s == "" {
			return ""
		}
		parts := strings.SplitN(s, ".", 3)
		if len(parts) == 1 {
			parts = append(parts, "0")
		}
		return parts[0] + "." + parts[1]
	}
	r := UserAgent{Browser: ua.Name, BrowserVersion: version(ua.Version), OS: ua.OS}
	switch {
	case ua.OS == useragent.Android:
		// Android tablets leave out "Mobile"; the parser counts them as phones.
		r.Device = DeviceTablet
		if strings.Contains(raw, "Mobile") {
			r.Device = DeviceMobile
		}
	case ua.Tablet:
		r.Device = DeviceTablet
	case ua.Mobile:
		r.Device = DeviceMobile
	case ua.Desktop:
		// iPads with Safari in desktop mode, the default, look like a Mac.
		r.Device = DeviceDesktop
	}
	if n, ok := browserNames[r.Browser]; ok {
		r.Browser = n
	}
	if n, ok := osNames[r.OS]; ok {
		r.OS = n
	}
	switch r.OS {
	case "GNU/Linux":
		// Plausible reports common distributions as their own OS.
		for _, distro := range []string{"Ubuntu", "Fedora", "Debian"} {
			if strings.Contains(raw, distro) {
				r.OS = distro
				break
			}
		}
	case "Windows", "Android":
		r.OSVersion = strings.TrimSuffix(version(ua.OSVersion), ".0")
	case "iOS":
		if strings.Contains(raw, "iPad") {
			r.OS = "iPadOS"
		}
		r.OSVersion = version(ua.OSVersion)
	case "Chrome OS":
	default:
		r.OSVersion = version(ua.OSVersion)
	}
	return r
}
