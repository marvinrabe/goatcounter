#!/bin/sh
#
# Fill a running GoatCounter instance with generated pageviews through /api.
#
# All statistics are derived from the "hits" table, exactly as cron/*_stat.go
# does it, so the dashboard sees the same thing it would after collecting this
# traffic for real. Note that the statistics count visits rather than
# pageviews: a pageview is only counted the first time a visit sees that path.
#
# WARNING: this deletes all existing pageviews and statistics.

set -eu

days=45
seed=1
visits=45
yes=0
api_url=${GOATCOUNTER_SAMPLE_API_URL:-http://localhost:8080/api}
api_token=${GOATCOUNTER_API_TOKEN:-sample-api-token}

usage() {
	cat <<EOF
Usage: ${0##*/} [-d days] [-n visits] [-s seed] [-u api-url] [-t token] [-y]

  -d  Days of history to generate; default $days.
  -n  Rough number of visits per day; default $visits. The actual number varies
      per day: weekends are quieter, traffic grows a bit towards today, and
      some days get a spike.
  -s  Seed for the random generator; the same seed gives the same data.
      Default $seed.
  -u  API endpoint; default $api_url.
  -t  API bearer token; default GOATCOUNTER_API_TOKEN or $api_token.
  -y  Don't ask for confirmation.
EOF
}

while getopts d:n:s:u:t:yh opt; do
	case $opt in
		d) days=$OPTARG   ;;
		n) visits=$OPTARG ;;
		s) seed=$OPTARG   ;;
		u) api_url=$OPTARG ;;
		t) api_token=$OPTARG ;;
		y) yes=1          ;;
		h) usage; exit 0  ;;
		*) usage >&2; exit 1 ;;
	esac
done

for n in "d:$days" "n:$visits" "s:$seed"; do
	case ${n#*:} in
		''|*[!0-9]*) echo "${0##*/}: -${n%%:*} must be a number: ${n#*:}" >&2; exit 1 ;;
	esac
done
[ "$days" -gt 0 ] && [ "$visits" -gt 0 ] || {
	echo "${0##*/}: -d and -n must be at least 1" >&2; exit 1; }

target=$api_url
command -v curl >/dev/null || { echo "${0##*/}: curl not found" >&2; exit 1; }

if [ "$yes" -eq 0 ]; then
	printf 'Replace all pageviews and statistics in %s with %s days of generated data? [y/N] ' "$target" "$days"
	reply=n
	read -r reply 2>/dev/null </dev/tty || true
	case $reply in
		y|Y|yes|YES) ;;
		*) echo 'Aborted.'; exit 1 ;;
	esac
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

cat >"$tmp/gen.awk" <<'AWK'
# Generate JSON objects accepted by the import_raw API action.
function jsonq(s) { gsub(/\\/, "\\\\", s); gsub(/\"/, "\\\"", s); return "\"" s "\"" }

function cumulate(w, n, cum,   i) {
	cum[1] = w[1]
	for (i = 2; i <= n; i++)
		cum[i] = cum[i - 1] + w[i]
}

function pick(cum, n,   r, i) {
	r = rand() * cum[n]
	for (i = 1; i <= n; i++)
		if (r <= cum[i])
			return i
	return n
}

function between(lo, hi) { return lo + int(rand() * (hi - lo + 1)) }

function session(   i, s) {
	s = ""
	for (i = 0; i < 4; i++)
		s = s sprintf("%08x", int(rand() * 4294967296))
	return s
}

function row(pi, ri, ci, di, wi, li, gi, first, sess, day, sec,   stamp, prefix) {
	stamp = base - day * 86400 + sec
	if (stamp > now)
		return
	prefix = pageviews++ ? "," : ""
	printf "%s{\"site\":%s,\"path\":%s,\"event\":%s,", prefix, jsonq(site[si]), jsonq(path[pi]), event[pi] ? "true" : "false"
	printf "\"ref\":%s,\"ref_scheme\":%s,\"campaign\":%s,", jsonq(ref[ri]), jsonq(scheme[ri]), ci ? jsonq(campaign[ci]) : "\"\""
	printf "\"browser\":%s,\"browser_version\":%s,\"system\":%s,\"system_version\":%s,", jsonq(browser[di]), jsonq(browser_version[di]), jsonq(osname[di]), jsonq(osversion[di])
	printf "\"location\":%s,\"language\":%s,\"width\":%d,\"first_visit\":%s,", jsonq(location[li]), jsonq(language[gi]), wi, first ? "true" : "false"
	printf "\"session\":%s,\"created_at_unix\":%d}", jsonq(sess), stamp
}

BEGIN {
	srand(seed)
	base = now - (hour * 3600 + minute * 60 + second)
	site[1] = "example.com"
	site[2] = "foobar.net"

	# path|event|weight as a landing page.
	npath = split(\
		"/|0|22;"                                      \
		"/blog|0|10;"                                  \
		"/blog/hello-world|0|14;"                      \
		"/blog/self-hosting-goatcounter|0|16;"         \
		"/blog/why-sqlite|0|11;"                       \
		"/blog/privacy-first-analytics|0|9;"            \
		"/docs|0|6;"                                   \
		"/docs/installation|0|7;"                      \
		"/docs/configuration|0|5;"                     \
		"/about|0|4;"                                  \
		"/contact|0|3;"                                \
		"download-brochure|1|0;"                       \
		"signup-click|1|0;"                            \
		"newsletter-subscribe|1|0",                    \
		p, ";")
	for (i = 1; i <= npath; i++) {
		split(p[i], f, "|")
		path[i]  = f[1]
		event[i] = f[2] + 0
		pw[i]    = f[3] + 0
		if (event[i])
			events[++nevent] = i
		else
			pages[++npage] = i
	}
	cumulate(pw, npath, pcum)

	# referrer|scheme|weight; the empty referrer (direct traffic) is ref_id 1
	# and always exists.
	nref = split(\
		"|o|100;"                      \
		"www.google.com|h|38;"         \
		"news.ycombinator.com|h|17;"   \
		"duckduckgo.com|h|9;"          \
		"github.com|h|8;"              \
		"www.reddit.com|h|7;"          \
		"lobste.rs|h|6;"               \
		"x.com|h|5;"                   \
		"mastodon.social|h|4;"         \
		"en.wikipedia.org|h|3",        \
		r, ";")
	for (i = 1; i <= nref; i++) {
		split(r[i], f, "|")
		ref[i] = f[1]; scheme[i] = f[2]; rw[i] = f[3] + 0
	}
	cumulate(rw, nref, rcum)

	# campaign|source|weight; campaign traffic has the source as its referrer.
	ncampaign = split(\
		"spring-launch|newsletter|10;" \
		"spring-launch|x|6;"           \
		"docs-push|newsletter|5;"      \
		"conf-talk|slides|4",          \
		c, ";")
	for (i = 1; i <= ncampaign; i++) {
		split(c[i], f, "|")
		campaign[i] = f[1]; campaign_ref[i] = f[2]; cw[i] = f[3] + 0
	}
	cumulate(cw, ncampaign, ccum)

	# browser|version|system|version|screen widths|weight.
	ndevice = split(\
		"Chrome|141|Windows|11|1920,1536,1366,2560|26;"   \
		"Chrome|140|Windows|10|1920,1366|8;"              \
		"Chrome|141|macOS|15.6|1512,1728,2560|7;"         \
		"Chrome|141|Android|15|360,393,412,800|13;"           \
		"Firefox|143|Linux||1920,2560,1280|6;"            \
		"Firefox|143|Windows|11|1920,1366|5;"             \
		"Safari|18.6|macOS|15.6|1512,1728,1440|11;"       \
		"Safari|18.6|iOS|18.6|390,414,430,768,820,1024|14;"            \
		"Edge|141|Windows|11|1920,1536|7;"                \
		"Samsung Internet|28|Android|14|360,412|3",       \
		d, ";")
	for (i = 1; i <= ndevice; i++) {
		split(d[i], f, "|")
		browser[i] = f[1]; browser_version[i] = f[2]
		osname[i]  = f[3]; osversion[i] = f[4]
		nwidth[i]  = split(f[5], w, ",")
		for (j = 1; j <= nwidth[i]; j++)
			width[i, j] = w[j] + 0
		dw[i] = f[6] + 0
	}
	cumulate(dw, ndevice, dcum)

	# code|country|region|country name|region name|weight.
	nlocation = split(\
		"DE|DE||Germany||30;"                     \
		"US|US||United States||10;"               \
		"US-CA|US|CA|United States|California|9;" \
		"US-NY|US|NY|United States|New York|7;"   \
		"GB|GB||United Kingdom||9;"               \
		"NL|NL||Netherlands||7;"                  \
		"AT|AT||Austria||6;"                      \
		"CH|CH||Switzerland||5;"                  \
		"FR|FR||France||5;"                       \
		"PL|PL||Poland||4;"                       \
		"CA|CA||Canada||4;"                       \
		"IN|IN||India||4;"                        \
		"SE|SE||Sweden||3;"                       \
		"BR|BR||Brazil||3;"                       \
		"JP|JP||Japan||2;"                        \
		"AU|AU||Australia||2",                    \
		l, ";")
	for (i = 1; i <= nlocation; i++) {
		split(l[i], f, "|")
		location[i]     = f[1]; country[i]      = f[2]; region[i] = f[3]
		country_name[i] = f[4]; region_name[i]  = f[5]
		lw[i] = f[6] + 0
	}
	cumulate(lw, nlocation, lcum)

	# ISO 639-3 code|weight, as Accept-Language is stored; see language.go.
	nlanguage = split(\
		"deu|30;eng|26;nld|8;fra|6;pol|5;spa|4;por|3;swe|3;ita|3;" \
		"ces|2;jpn|2;hin|2;zho|2;rus|2;|3",                        \
		g, ";")
	for (i = 1; i <= nlanguage; i++) {
		split(g[i], f, "|")
		language[i] = f[1]
		gw[i] = f[2] + 0
	}
	cumulate(gw, nlanguage, gcum)

	# Visits per hour of the day, from 00:00 to 23:00.
	nhour = split("2 1 1 1 1 2 4 7 11 15 18 19 17 18 19 18 16 14 12 11 9 7 5 3", h, " ")
	for (i = 1; i <= nhour; i++)
		hw[i] = h[i] + 0
	cumulate(hw, nhour, hcum)

	# Pageviews in a single visit.
	npages_in_visit = split("52 22 12 7 4 3", v, " ")
	for (i = 1; i <= npages_in_visit; i++)
		vw[i] = v[i] + 0
	cumulate(vw, npages_in_visit, vcum)

	for (si = 1; si <= 2; si++) {
	for (day = days - 1; day >= 0; day--) {
		# 1 (Monday) through 7 (Sunday).
		dow = (today_dow - 1 - day) % 7
		if (dow < 0)
			dow += 7
		dow++

		n = visits
		n *= (dow >= 6) ? 0.55 : 1                                  # Quieter weekends.
		n *= 0.75 + 0.5 * (days > 1 ? (days - 1 - day) / (days - 1) : 1)  # Slowly growing traffic.
		n *= 0.8 + rand() * 0.45                                    # Noise.
		if (rand() < 0.055)                                         # The occasional good day.
			n *= 2.2 + rand()
		n = int(n + 0.5)

		for (i = 0; i < n; i++) {
			sess = session()
			di   = pick(dcum, ndevice)
			wi   = width[di, between(1, nwidth[di])]
			li   = pick(lcum, nlocation)
			gi   = pick(gcum, nlanguage)
			sec  = (pick(hcum, nhour) - 1) * 3600 + int(rand() * 3600)

			ci = 0
			ri = 1
			if (rand() < 0.09) {           # Campaign traffic.
				ci = pick(ccum, ncampaign)
				ri = -ci                   # Resolved below.
			} else
				ri = pick(rcum, nref)

			# A pageview is a "first visit" the first time a session sees
			# that path, which is what the statistics count; see
			# memstore.go:session().
			delete seen

			npv = pick(vcum, npages_in_visit)
			for (j = 1; j <= npv; j++) {
				pi = (j == 1) ? pick(pcum, npath) : pages[between(1, npage)]
				if (j > 1)
					sec += between(20, 420)
				first = (pi in seen) ? 0 : 1
				seen[pi] = 1
				if (ri < 0)
					campaign_row(pi, -ri, di, wi, li, gi, first, sess, day, sec)
				else
					row(pi, ri, ci, di, wi, li, gi, first, sess, day, sec)
			}

			if (nevent > 0 && rand() < 0.12) {
				sec += between(20, 420)
				pi = events[between(1, nevent)]
				first = (pi in seen) ? 0 : 1
				seen[pi] = 1
				if (ri < 0)
					campaign_row(pi, -ri, di, wi, li, gi, first, sess, day, sec)
				else
					row(pi, ri, ci, di, wi, li, gi, first, sess, day, sec)
			}
			visit_count++
		}
	}
	}
	printf "\n    %d visits, %d pageviews over %d days\n", visit_count, pageviews, days > "/dev/stderr"
}

# Campaign traffic: the referrer is the campaign source, with the "c" scheme.
function campaign_row(pi, ci, di, wi, li, gi, first, sess, day, sec,   oldref, oldscheme) {
	oldref = ref[0]; oldscheme = scheme[0]
	ref[0] = campaign_ref[ci]; scheme[0] = "c"
	row(pi, 0, ci, di, wi, li, gi, first, sess, day, sec)
	ref[0] = oldref; scheme[0] = oldscheme
}
AWK

echo "==> Generating $days days of pageviews"
{
	printf '{"action":"import_raw","arguments":{"replace":true,"hits":['
	TZ=UTC awk -v days="$days" -v visits="$visits" -v seed="$seed" -v today_dow="$(date +%u)" \
		-v now="$(date +%s)" -v hour="$(date -u +%H)" -v minute="$(date -u +%M)" -v second="$(date -u +%S)" \
		-f "$tmp/gen.awk"
	printf ']}}\n'
} >"$tmp/sample.json"

echo '==> Importing through the bearer-protected API'
curl --fail-with-body --silent --show-error \
	-H "Authorization: Bearer $api_token" \
	-H 'Content-Type: application/json' \
	--data-binary "@$tmp/sample.json" \
	"$api_url"

echo "Done. Dashboard: ${api_url%/api}/"
