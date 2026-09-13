// Bot signature data from isbot v1.1.0; see LICENSE and README.md.
package bot

var clientLibraries = []string{
	"Go-http-client/",
	"HttpClient/",
	"HTTPClient/",
	"Java/",
	"PycURL/",
	"Python-urllib/",
	"Robosourcer/",
	"Ruby",
	"Wget/",
	"Wget/",
	"WinHttp.WinHttpRequest.5",
	"curl/",
	"python-requests/",
	"libwww-perl/",
}

var knownBrowsers = []string{
	"CUBOT_",
	"CUBOT ",
	"StudoBrowser/",
}

var knownBots = []string{
	"ADmantX",
	"AlexaToolbar/",
	"BingPreview/",
	"Chrome-Lighthouse",
	"DumpRenderTree/",
	"Faraday v",
	"GigablastOpenSource/",
	// TODO: Just checking for "Google" might actually work; hmm...
	"Google Web Preview",
	"Google favicon",
	"Google-Ad",
	"Google-Site-Verification",
	"GoogleSecurityScanner",
	"Google_Analytics_Snippet_Validator",
	"HeadlessChrome/",
	"Netcraft Web Server Survey",
	"NetcraftSurveyAgent/",
	"Owler/",
	"PageAnalyzer/",
	"ScopeContentAG-HTTP-Client",
	"Survey/",
	"Synapse",
	"Wappalyzer",
	"WhatWeb/",
	"WinInet",
	"WordPress.com",
	"burpcollaborator.net/", // Burp security analyzer
	"okhttp/",
	"panscient.com",
	"tracemyfile/",
	"wsr-agent/",
	"RuxitRecorder/",     // Dynatrace performance monitor.
	"RuxitSynthetic/",    // Dynatrace performance monitor.
	"TrendsmapResolver/", // ?
	"ubermetrics-technologies.com",
	"zgrab/",                     //  https://github.com/zmap/zgrab2/search?q=user-agent
	"nbertaupete95(at)gmail.com", // Not sure what this belongs to
	"Dataprovider.com",
	"wkhtmltoimage", "wkhtmltopdf",
	"SlimerJS",
}
