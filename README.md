This is a heavily trimmed-down fork of [GoatCounter][www] for our own sites. It
replaces Plausible: the data model and counting rules follow Plausible, and a
Plausible CSV export is migrated into the database once, so the dashboard shows the full
history.

What's left is the `count.js` tracking script, the `/count` endpoint, and the
dashboard. There is no API, no export, no user management, no settings page,
no email, no translations, and no background jobs.

[www]: https://www.goatcounter.com


How data is stored
------------------
All data is in one table, `events` (see [db/schema.sql](db/schema.sql)),
plus `salts` for the daily random salts of visitor hashes (older than a day
are deleted).

A collected row is one pageview or custom event, stored the way Plausible
stores them: the event `name` ("pageview" or a custom name), `hostname`
without "www.", the URL `path` as sent (decoded, without query string, a
trailing slash kept), custom event `props`, `source` ("Google", "LinkedIn",
"news.ycombinator.com" or the `utm_source`), `referrer` (host and path), all
five UTM parameters, browser and OS with major.minor versions, screen width,
country, and language.

The Plausible history is in the same table, with the same columns. A
Plausible export isn't a list of visits but a separate daily total for each
dimension (visits per browser per day, visits per source per day, …), so a
migrated row is one of those totals: `aggregate` says which export file it's
from, Plausible's counts are in the `visits`, `pageviews`, `bounces`, …
columns, and its `ts` is midnight of that day. Collected rows have
`aggregate = ''`. This keeps every number as Plausible reported it, without
inventing which browser came with which source.

The dashboard calculates everything from this table at query time. Nothing
else is written, and nothing runs in the background.

### Counting rules

These are the same as Plausible's:

- A **visitor** is a hash of a daily salt, the site, the IP address and the
  User-Agent. The IP address is never stored, and once a salt is deleted a
  hash can't be linked to anyone. Visitors are unique per day.
- A **visit** ends after 30 minutes of inactivity, and continues across
  midnight. It takes its source, browser, location, etc. from its first
  pageview.
- A **bounce** is a visit with one pageview and no custom events; the **visit
  duration** is the time between its first and last event.
- **Pages** and **events** count unique visitors, the other breakdowns count
  visits.

With a path filter, only matching events are considered. Migrated data then
only contributes to the totals and the page list: Plausible doesn't export
other dimensions by page.


Tracking
--------

    <script data-site="example.com"
            async src="//stats.example.com/count.js"></script>

`count.js` sends data to `count` in the same directory it was loaded from;
override this with `data-endpoint`, such as `data-endpoint="//foobar.com/events"`.
It sends a `POST` with `navigator.sendBeacon()`, or `fetch()` if that fails, and
the response is always an empty `204`.

The script counts one pageview when the page first becomes visible and another
when the browser restores it from the back/forward cache. It sends the
hostname, path and query string, the referrer, the screen width, and an
automation flag. The UTM parameters are read from the query string, which
isn't stored.

Once the script has loaded, use `window.goatcounter.count()` to count another
pageview, for example in a single-page app after changing the URL. Count a
custom event with `window.goatcounter.count({event: 'Signup', props: {plan: 'pro'}})`;
it's recorded for the current page. The other options are `path`, `referrer`,
`no_session`, and `site`.

Clicks on links to other sites are counted as `Outbound Link: Click` events
with a `url` property, as with Plausible's outbound link tracking. Set
`window.goatcounter = {no_outbound: true}` before loading the script to
disable this.

To disable the automatic pageview, set `no_onload: true` in the same object.
Local addresses and frames are skipped by default; set `allow_local: true` or
`allow_frame: true` to enable them.


Running
-------

    % GOATCOUNTER_DB=libsql://db.example.com?authToken=TOKEN \
      GOATCOUNTER_SITES=example.com,foobar.net \
      TZ=Europe/Berlin \
      goatcounter serve

`GOATCOUNTER_DB` is a remote libSQL URL or a local SQLite path; an empty
database is initialized on startup. `GOATCOUNTER_SITES` lists the sites that
are accepted and shown; the name is stored with every row. `TZ` is the
dashboard timezone, for everyone (default UTC). Use `goatcounter serve -h` for
all settings; each flag can be set as `GOATCOUNTER_«FLAG»`.

GoatCounter serves plain HTTP; TLS is terminated by the CDN in front.

### CDN headers

GoatCounter is made to run behind bunny.net's CDN, and trusts the headers it
sets for the visitor's IP (`X-Real-IP`), country (`CDN-RequestCountryCode`),
and whether they connected with TLS (`X-Forwarded-Proto`). As anyone can send
these headers, all traffic must go through the CDN. They're all defined in
`internal/enrich/cdn.go`; that's the place to change for another CDN.

### Multiple containers and regions

Containers are stateless, so any number can run in any region, as long as they
all use the same libSQL database. Every pageview is a single `INSERT`; the
visit is resolved in the same statement from the visitor's most recent
pageview, so no transaction, queue, or sticky session is needed, and an idle
container makes no database requests. Each container reads the daily salt
once a day.

`/count` is limited to 4 requests per second per IP and User-Agent, per
container.

`/status` is the health check; it verifies database connectivity. On SIGTERM a
container finishes requests within 25 seconds. The Docker image has a
`HEALTHCHECK` running `goatcounter healthcheck`.

### Dashboard authentication

Choose a mode with `GOATCOUNTER_AUTH`:

- `public` (the default): anyone who can reach the server can view it.
- `basic`: set `GOATCOUNTER_BASIC_AUTH=alice:password,bob:password`.
- `oidc`: register a confidential web client, then set:

      GOATCOUNTER_OIDC_ISSUER=https://id.example.com
      GOATCOUNTER_OIDC_CLIENT_ID=goatcounter
      GOATCOUNTER_OIDC_CLIENT_SECRET=provider-client-secret
      GOATCOUNTER_OIDC_SESSION_SECRET=a-random-secret-containing-at-least-32-bytes

  Register `https://«your domain»/auth/callback` as the redirect URL; it's
  built from the domain the dashboard is opened on. Only the `openid` scope is
  requested. Changing the session secret logs everyone out. The login is a
  signed cookie, so it works on every container.

### Locations

The country comes from bunny.net's `CDN-RequestCountryCode` header, which it
sends on every request through a pull zone; the "Vary" country options are
about caching and aren't needed. Without the header, locations aren't
recorded. Only the country is recorded; the Plausible migration drops regions
and cities too.

### Bot and referrer spam lists

Bots are recognized by their User-Agent and by coming from a hosting
provider's network; referrer spam from Matomo's list. All lists are built in,
and the ones with an upstream – Matomo, and the networks of AWS, Google Cloud,
Oracle, DigitalOcean, and Linode – are downloaded once a day. A failed download
keeps the lists in use and is retried later. `internal/enrich` has all lookups, a file for each.

### Docker

    % docker build -t goatcounter .
    % docker run -p 8080:8080 \
        -e GOATCOUNTER_DB=libsql://db.example.com?authToken=TOKEN \
        -e GOATCOUNTER_SITES=example.com \
        goatcounter

`compose.yaml` runs the app with a local libSQL server, for testing.


Migrating from Plausible
------------------------
The Plausible history is moved into the database once, with a separate tool
that's not part of the server or the Docker image:

    % TZ=Europe/Berlin go run ./tools/plausible-migrate -db "$GOATCOUNTER_DB" -site example.com export.zip

`TZ` must be the dashboard's timezone, which should be the timezone Plausible
used for the export. All 11 CSV files are migrated in one transaction, which
replaces earlier migrated rows for that site; collected events aren't touched.
Values are stored as exported, which is how the collector stores new data too;
only the column names are mapped (`page` → `path`, `operating_system` → `os`,
`link_url` → `props`, …).


Schema changes
--------------
There are no migrations: a database with an older schema is refused, and you
start with a new database and import again.


Building from source
--------------------
You need Go 1.27 or newer and Node.js 22.12 or newer. Everything is pure Go,
including SQLite ([modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)),
so no C compiler is needed and `CGO_ENABLED=0` builds a static binary.

    % npm ci
    % npm run build
    % go build ./cmd/goatcounter

JavaScript sources and tests are in `assets/js/`, stylesheets in `assets/css/`.
`public/` is generated by the frontend build and embedded in the binary.
