#!/bin/sh
#
# Fill the database with generated pageviews and create a user to log in with,
# so there's something to look at on the dashboard while developing.
#
# By default it works on the "goatcounter" service from compose.yaml in this
# directory (http://localhost:8080); the service is stopped while the data is
# written and started again afterwards. Use -f to work on a SQLite file
# directly instead, e.g. when running "goatcounter serve" outside of Docker.
#
# All statistics are derived from the "hits" table, exactly as cron/*_stat.go
# does it, so the dashboard sees the same thing it would after collecting this
# traffic for real. Note that the statistics count visits rather than
# pageviews: a pageview is only counted the first time a visit sees that path.
#
# WARNING: this deletes all existing pageviews and statistics.

set -eu

dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

days=45
email=admin@example.com
password=password
seed=1
visits=45
dbfile=
bin=
yes=0

service=goatcounter
service_db=/home/goatcounter/goatcounter-data/db.sqlite3

usage() {
	cat <<EOF
Usage: ${0##*/} [-d days] [-n visits] [-s seed] [-e email] [-p password]
                      [-f db-file] [-b binary] [-y]

  -d  Days of history to generate; default $days.
  -n  Rough number of visits per day; default $visits. The actual number varies
      per day: weekends are quieter, traffic grows a bit towards today, and
      some days get a spike.
  -s  Seed for the random generator; the same seed gives the same data.
      Default $seed.
  -e  Email address of the sample user; default $email.
  -p  Password of the sample user, at least 8 characters; default $password.
      Any username works when logging in, only the password is checked.
  -f  Write to this SQLite file instead of the docker compose service; the
      server should not be running while this script writes to it.
  -b  goatcounter binary to run the database commands with; only used with -f.
      Defaults to ./goatcounter, or "go run ./cmd/goatcounter" if that's absent.
  -y  Don't ask for confirmation.
EOF
}

while getopts d:n:s:e:p:f:b:yh opt; do
	case $opt in
		d) days=$OPTARG   ;;
		n) visits=$OPTARG ;;
		s) seed=$OPTARG   ;;
		e) email=$OPTARG  ;;
		p) password=$OPTARG ;;
		f) dbfile=$OPTARG ;;
		b) bin=$OPTARG    ;;
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

# Run a goatcounter command against the database.
gc() {
	if [ -z "$dbfile" ]; then
		docker compose -f "$dir/compose.yaml" run --rm -T "$service" \
			"$@" -db "sqlite+$service_db" -createdb
	elif [ -n "$bin" ]; then
		"$bin" "$@" -db "sqlite+$dbfile" -createdb
	else
		(cd "$dir" && go run ./cmd/goatcounter "$@" -db "sqlite+$dbfile" -createdb)
	fi
}

if [ -n "$dbfile" ]; then
	target=$dbfile
	[ -n "$bin" ] || [ ! -x "$dir/goatcounter" ] || bin=$dir/goatcounter
else
	target="docker compose service \"$service\""
	command -v docker >/dev/null || { echo "${0##*/}: docker not found; use -f to write to a SQLite file" >&2; exit 1; }
fi

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

# Stop the server: it caches the site (including the date of the first
# pageview, which limits how far back the dashboard looks) and it's the only
# writer the database should have while we replace its contents.
started=
if [ -z "$dbfile" ] && [ -n "$(docker compose -f "$dir/compose.yaml" ps -q --status running "$service")" ]; then
	started=yes
	echo '==> Stopping the server'
	docker compose -f "$dir/compose.yaml" stop "$service" >/dev/null
fi

echo "==> Creating the user $email"
if out=$(gc db create user -email "$email" -password "$password" 2>&1); then
	:
else
	case $out in
		*'already used by another user'*)
			echo '    User already exists; setting the password'
			gc db update user -find "$email" -password "$password" >/dev/null
			;;
		*) printf '%s\n' "$out" >&2; exit 1 ;;
	esac
fi

cat >"$tmp/gen.awk" <<'AWK'
# Generate SQL with sample pageviews. Dates are left to SQLite ("now" minus a
# number of days and seconds), so this doesn't need date(1), which differs
# between BSD and GNU.

function q(s) { gsub(/'/, "''", s); return "'" s "'" }

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

function row(pi, ri, ci, di, wi, li, gi, first, sess, day, sec,   v) {
	v = sprintf("(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%d,%d,X'%s'," \
		"datetime('now','start of day','-%d days','+%d seconds'))",
		q(path[pi]), q(ref[ri]), q(scheme[ri]),
		ci ? q(campaign[ci]) : "null",
		q(browser[di]), q(browser_version[di]), q(osname[di]), q(osversion[di]),
		q(location[li]), q(language[gi]), wi, first, sess, day, sec)

	batch = (nbatch++ == 0) ? v : batch "," v
	if (nbatch >= 100)
		flush()
	pageviews++
}

function flush() {
	if (nbatch > 0)
		print "insert into sample_hits values " batch ";"
	nbatch = 0
	batch = ""
}

BEGIN {
	srand(seed)

	# path|title|event|weight as a landing page.
	npath = split(\
		"/|Home|0|22;"                                                  \
		"/blog|Blog|0|10;"                                              \
		"/blog/hello-world|Hello, world!|0|14;"                         \
		"/blog/self-hosting-goatcounter|Self-hosting GoatCounter|0|16;" \
		"/blog/why-sqlite|Why SQLite is enough|0|11;"                   \
		"/blog/privacy-first-analytics|Privacy-first analytics|0|9;"    \
		"/docs|Documentation|0|6;"                                      \
		"/docs/installation|Installation|0|7;"                          \
		"/docs/configuration|Configuration|0|5;"                        \
		"/about|About|0|4;"                                             \
		"/contact|Contact|0|3;"                                         \
		"download-brochure|Download brochure|1|0;"                      \
		"signup-click|Signup button|1|0;"                               \
		"newsletter-subscribe|Newsletter subscribe|1|0",                \
		p, ";")
	for (i = 1; i <= npath; i++) {
		split(p[i], f, "|")
		path[i]  = f[1]; title[i] = f[2]
		event[i] = f[3] + 0
		pw[i]    = f[4] + 0
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

	print "begin;"

	print "delete from hits;"
	print "delete from bots;"
	print "delete from hit_counts;"
	print "delete from ref_counts;"
	print "delete from browser_stats;"
	print "delete from system_stats;"
	print "delete from location_stats;"
	print "delete from language_stats;"
	print "delete from size_stats;"
	print "delete from campaign_stats;"

	for (i = 1; i <= npath; i++)
		printf "insert or ignore into paths (path, title, event) values (%s, %s, %d);\n",
			q(path[i]), q(title[i]), event[i]
	for (i = 1; i <= nref; i++)
		printf "insert or ignore into refs (ref, ref_scheme) values (%s, %s);\n",
			q(ref[i]), q(scheme[i])
	for (i = 1; i <= ncampaign; i++)
		printf "insert into campaigns (name) select %s where not exists " \
			"(select 1 from campaigns where name = %s);\n", q(campaign[i]), q(campaign[i])
	for (i = 1; i <= ncampaign; i++)
		printf "insert or ignore into refs (ref, ref_scheme) values (%s, 'c');\n", q(campaign_ref[i])
	for (i = 1; i <= ndevice; i++) {
		printf "insert or ignore into browsers (name, version) values (%s, %s);\n",
			q(browser[i]), q(browser_version[i])
		printf "insert or ignore into systems (name, version) values (%s, %s);\n",
			q(osname[i]), q(osversion[i])
	}
	for (i = 1; i <= nlocation; i++)
		printf "insert or ignore into locations (country, region, country_name, region_name) " \
			"values (%s, %s, %s, %s);\n",
			q(country[i]), q(region[i]), q(country_name[i]), q(region_name[i])

	print "create temp table sample_hits (path text, ref text, ref_scheme text," \
		" campaign text, browser text, browser_version text, system text," \
		" system_version text, location text, language text, width int, first_visit int," \
		" session blob, created_at text);"

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
	flush()

	print "insert into hits (path_id, ref_id, session, first_visit, browser_id," \
		" system_id, campaign, width, location, language, created_at)"
	print "select p.path_id, r.ref_id, s.session, s.first_visit, b.browser_id," \
		" y.system_id, c.campaign_id, s.width, s.location," \
		" nullif(s.language, ''), s.created_at"
	print "from sample_hits s"
	print "join paths p on lower(p.path) = lower(s.path)"
	print "join refs r on lower(r.ref) = lower(s.ref) and r.ref_scheme = s.ref_scheme"
	print "join browsers b on b.name = s.browser and b.version = s.browser_version"
	print "join systems y on y.name = s.system and y.version = s.system_version"
	print "left join campaigns c on c.name = s.campaign;"
	print "drop table sample_hits;"

	# Today is generated as a full day; drop what hasn't happened yet.
	print "delete from hits where created_at > strftime('%Y-%m-%d %H:%M:%S', 'now');"

	# The statistics count visits, not pageviews: a pageview is counted only
	# the first time a visit sees that path. See cron/*_stat.go.
	print "insert into hit_counts (path_id, hour, total)"
	print "select path_id, strftime('%Y-%m-%d %H:00:00', created_at), count(*)"
	print "from hits where first_visit = 1 group by 1, 2;"

	print "insert into ref_counts (path_id, ref_id, hour, total)"
	print "select path_id, ref_id, strftime('%Y-%m-%d %H:00:00', created_at), count(*)"
	print "from hits where first_visit = 1 group by 1, 2, 3;"

	print "insert into browser_stats (path_id, browser_id, day, count)"
	print "select path_id, browser_id, date(created_at), count(*)"
	print "from hits where first_visit = 1 and browser_id > 0 group by 1, 2, 3;"

	print "insert into system_stats (path_id, system_id, day, count)"
	print "select path_id, system_id, date(created_at), count(*)"
	print "from hits where first_visit = 1 and system_id > 0 group by 1, 2, 3;"

	print "insert into location_stats (path_id, day, location, count)"
	print "select path_id, date(created_at), location, count(*)"
	print "from hits where first_visit = 1 group by 1, 2, 3;"

	print "insert into language_stats (path_id, day, language, count)"
	print "select path_id, date(created_at), coalesce(language, ''), count(*)"
	print "from hits where first_visit = 1 group by 1, 2, 3;"

	print "insert into size_stats (path_id, day, width, count)"
	print "select path_id, date(created_at), coalesce(width, 0), count(*)"
	print "from hits where first_visit = 1 group by 1, 2, 3;"

	print "insert into campaign_stats (path_id, day, campaign_id, ref, count)"
	print "select h.path_id, date(h.created_at), h.campaign, r.ref, count(*)"
	print "from hits h join refs r on r.ref_id = h.ref_id"
	print "where h.first_visit = 1 and h.campaign is not null group by 1, 2, 3, 4;"

	# The site row is normally created on the first request; create it here so
	# the dashboard doesn't limit the range to a week before it was created.
	print "insert into site (link_domain, settings, received_data, created_at, first_hit_at)"
	print "select '', '{}', 0, datetime('now'), datetime('now')"
	print "where not exists (select 1 from site);"
	print "update site set received_data = 1," \
		" first_hit_at = (select datetime(min(created_at), '-12 hours') from hits),"
	# Collecting languages is off by default, but the data above has them, so
	# switch it on to keep the dashboard consistent. 190 is the default set of
	# flags, 64 is CollectLanguage; see settings.go.
	print " settings = json_set(settings, '$.collect'," \
		" coalesce(json_extract(settings, '$.collect'), 190) | 64);"

	print "commit;"

	printf "    %d visits, %d pageviews over %d days\n", visit_count, pageviews, days > "/dev/stderr"
}

# Campaign traffic: the referrer is the campaign source, with the "c" scheme.
function campaign_row(pi, ci, di, wi, li, gi, first, sess, day, sec,   v) {
	v = sprintf("(%s,%s,'c',%s,%s,%s,%s,%s,%s,%s,%d,%d,X'%s'," \
		"datetime('now','start of day','-%d days','+%d seconds'))",
		q(path[pi]), q(campaign_ref[ci]), q(campaign[ci]),
		q(browser[di]), q(browser_version[di]), q(osname[di]), q(osversion[di]),
		q(location[li]), q(language[gi]), wi, first, sess, day, sec)

	batch = (nbatch++ == 0) ? v : batch "," v
	if (nbatch >= 100)
		flush()
	pageviews++
}
AWK

echo "==> Generating $days days of pageviews"
awk -v days="$days" -v visits="$visits" -v seed="$seed" -v today_dow="$(date +%u)" \
	-f "$tmp/gen.awk" >"$tmp/sample.sql"

echo '==> Writing to the database'
gc db query -format=exec <"$tmp/sample.sql"
gc db query -format=table "select
	(select count(*) from hits)                         as pageviews,
	(select sum(total) from hit_counts)                 as visits,
	(select count(distinct date(created_at)) from hits) as days,
	(select date(min(created_at)) from hits)            as first,
	(select date(max(created_at)) from hits)            as last"

if [ -n "$started" ]; then
	echo '==> Starting the server'
	docker compose -f "$dir/compose.yaml" up -d >/dev/null
fi

if [ -n "$dbfile" ]; then
	cat <<EOF

Done. Log in with any username and the password "$password".
EOF
else
	cat <<EOF

Done. Log in at http://localhost:8080/ with any username and the password
"$password".
EOF
fi
