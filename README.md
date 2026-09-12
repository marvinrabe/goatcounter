This is a heavily trimmed-down fork of [GoatCounter][www] for self-hosting a
multiple explicitly configured sites with low CPU and RAM usage. What's left is the
`count.js` tracking script, the collection endpoint, and the dashboard; most of
what makes upstream a hosted, multi-tenant, multi-language product is gone.

Collecting data:

- The old `/api/v0` API and database-backed API tokens are gone. A compact
  JSON/MCP endpoint at `/api` is available when `GOATCOUNTER_API_TOKEN` is set.
- No CLI log-file import (`goatcounter import`); raw historical data can be
  loaded through `/api`.
- No export: neither the CSV/JSON export pages nor the background export jobs.
- No visitor counter (the `/counter/…` images and HTML fragment).
- Only the current `count.js` is served; the pinned `count.v1.js` …
  `count.v5.js` copies are gone. The `/count` endpoint itself is unchanged, so
  tracking with an `<img>` pixel still works — it's just not documented here.

Running it:

- SQLite only; PostgreSQL support is gone, including all PostgreSQL-specific
  queries and migrations.
- No TLS and no ACME: it serves plain HTTP and expects a reverse proxy in
  front.
- No multi-tenant/SaaS site management: sites come from the comma-separated
  `GOATCOUNTER_SITES` environment variable. There is no `sites` table or site
  management UI; each data row stores the configured site name as its key.
- No email at all: no SMTP, no email reports, no password resets, no email
  verification, no "email" debug pages.
- English only: the `i18n/` translations, the translation UI, and the
  per-user locale are gone. Dates, numbers, and times use international formats
  (ISO dates, 24-hour clock, thin-space thousands separator).
- Only four commands: `serve`, `healthcheck`, `help`, and `version`. Use external
  SQLite/libSQL tools for database administration. Empty databases are initialized
  automatically by `serve`.
- No runtime metrics collection and no admin ("bosmang") pages for cache,
  background jobs, GeoIP, and metrics.

Web interface:

- Dashboard access has three modes: public, HTTP Basic authentication, or
  OpenID Connect (OIDC). Authentication is configured entirely through
  environment variables; there is no user table or user management.
- No user or site settings page. The dashboard layout is fixed (totals first),
  the timezone comes from `TZ`, all supported data is collected, retention is
  forever.
- No signup, no user management UI, no site deletion, or changing the site
  code.
- No marketing website (home, "why", contact, contribute, design) and no
  in-app help pages.
- No no footer menu and no custom date picker — the browser's
  native date input is used instead.
- No third-party front-end code: jQuery, dragula, and pikaday are gone, as are
  the bundled Lato webfonts (a system font stack is used).
- No dashboard websocket loader; widgets are rendered with the page and only
  paging and drill-downs fetch more.

The dashboard is displayed in the timezone from the `TZ` environment variable
(e.g. `TZ=Europe/Berlin`), for everyone; it defaults to UTC.

[www]: https://www.goatcounter.com


Features
--------
- **Privacy-aware**; doesn’t track users with unique identifiers and doesn't
  need a GDPR notice.

- **Lightweight** and **fast**; the tracking script is about 0.6 KB gzipped.

- Identify **unique visits** without cookies using a non-identifiable hash.

- Keeps useful statistics such as **browser** information, **location**,
  **language**, and **screen size**. Keep track of **referring sites** and
  **campaigns**.

- **Own your data**; everything is in a single SQLite database.

- Integrate on your site with just a **single script tag**:

      <script data-site="example.com"
              async src="//stats.example.com/count.js"></script>

  By default, `count.js` sends data to `count` in the same directory it was
  loaded from. For example, `https://stats.example.com/count.js` uses
  `https://stats.example.com/count`. Override this when needed with
  `data-endpoint`, such as `data-endpoint="//foobar.com/events"`.

The script counts one pageview when the page first becomes visible. It sends
the current path and query string, referrer, screen width, and an automation
flag. It does not read canonical URLs or page titles.

Once the script has loaded, use `window.goatcounter.count()` to count another
pageview, or `window.goatcounter.count({path: 'download', event: true})` to count
an event. The optional fields are `path`, `referrer`, `event`, `no_session`, and
`site`. Use strings for paths, referrers, and sites, and booleans for the flags.
Single-page apps should call `count()` after changing the URL.

To disable the automatic pageview, set `window.goatcounter = {no_onload: true}`
before loading the script. Local addresses and frames are skipped by default;
set `allow_local: true` or `allow_frame: true` in the same object to enable them.
An `endpoint` in that object is also supported; `data-endpoint` takes precedence.

This minimal tracker replaces the upstream JavaScript API. JSON settings in
`data-goatcounter-settings`, automatic `data-goatcounter-click` bindings,
path/referrer callbacks, and the old helper methods are no longer supported.
Use `count()` directly for custom tracking.


Self-hosting GoatCounter
------------------------
The only dependency is somewhere to store a local SQLite-compatible database
file, or access to a remote libSQL database. Alternatively you can use Docker,
as documented in the section below.

### Running
You can start a server with:

    % goatcounter serve

This will start a server on `*:8080`. The default is to use a local database at
`/data/goatcounter.db`, which will be created if it doesn't exist yet. Set
`GOATCOUNTER_DB` to a `libsql://` URL to use a remote libSQL service.

Set the sites before starting the server:

    % export GOATCOUNTER_SITES=example.com,foobar.net

Dashboard access is public by default. Choose one of the authentication modes
below with `GOATCOUNTER_AUTH`.

#### Public

Anyone who can reach the server can view the dashboard:

    GOATCOUNTER_AUTH=public

#### HTTP Basic authentication

Define one or more `username:password` pairs in the environment. Both the
username and password must match; separate multiple users with commas.

    GOATCOUNTER_AUTH=basic
    GOATCOUNTER_BASIC_AUTH=alice:correct-horse-battery-staple,bob:another-long-password

Credentials are held in process memory and are never written to SQLite. Since
commas separate users, passwords cannot contain commas. Put TLS in front of
GoatCounter before using Basic authentication.

#### OpenID Connect

Register GoatCounter as a confidential web client with the OIDC provider, then
set the following values:

    GOATCOUNTER_AUTH=oidc
    GOATCOUNTER_OIDC_ISSUER=https://id.example.com
    GOATCOUNTER_OIDC_CLIENT_ID=goatcounter
    GOATCOUNTER_OIDC_CLIENT_SECRET=provider-client-secret
    GOATCOUNTER_OIDC_REDIRECT_URL=https://stats.example.com/auth/callback
    GOATCOUNTER_OIDC_SESSION_SECRET=a-random-secret-containing-at-least-32-bytes

The redirect URL must be registered verbatim with the provider and must point
to `/auth/callback` below GoatCounter's base path. Generate a separate session
secret, for example with `openssl rand -base64 32`; changing it logs out all
current dashboard sessions. The optional `GOATCOUNTER_OIDC_SCOPES` value adds
comma-separated scopes to the required `openid` scope.

OIDC uses Authorization Code flow, validates the ID token and nonce, and keeps
only a signed, short-lived authentication session in the browser. No OIDC
profile is stored or passed into dashboard view logic.

These modes protect only the web dashboard. The `/api` endpoint continues to
use its separate bearer-token authentication.

GoatCounter serves plain HTTP; put a reverse proxy (nginx, Caddy, …) in front
of it for TLS.

### Running with Docker
There is no published image for this fork; the images on DockerHub are
upstream's, with everything listed above still in them. Build it yourself:

    % docker build -t goatcounter .
    % docker run \
        -p 8080:8080 \
        -v goatcounter-data:/data \
        -e GOATCOUNTER_SITES=example.com,foobar.net \
        goatcounter

This uses a named volume, which is recommended as this stores the SQLite
database and anonymous volumes can be easy to accidentally delete.

`compose.yaml` has a ready-to-run example, including the memory limits and
`GOGC`/`GOMEMLIMIT` settings this fork is tuned for.

### Management
A status URL is available at `/status`, which can be used for health monitors.

### API and MCP

GoatCounter provides one endpoint for both ordinary JSON requests and MCP:
`/api`, below the configured base path. Set a bearer token to enable it:

    GOATCOUNTER_API_TOKEN=a-long-random-secret

If the value is unset or empty, the route is not registered and returns 404.
Every API and MCP request must include the following header:

    Authorization: Bearer a-long-random-secret

This authentication is deliberately independent from the dashboard. Public
dashboard access, a valid Basic login, and a valid OIDC session never authorize
an API request. Use TLS in front of GoatCounter so the bearer token is not sent
over an unencrypted network.

An authenticated `GET /api` returns endpoint information and the JSON Schema
for every supported action. Requests containing an `Origin` header are accepted
only when its host matches the request host.

#### JSON API

Send `POST /api` with `Content-Type: application/json`. The request envelope is:

    {
      "action": "sites",
      "arguments": {}
    }

Successful responses use `{"result": ...}`. Failed requests use
`{"error":"..."}` with an appropriate HTTP status. Request bodies are limited
to 64 MiB and unknown fields inside `arguments` are rejected.

For example:

    curl https://stats.example.com/api \
        -H "Authorization: Bearer $GOATCOUNTER_API_TOKEN" \
        -H 'Content-Type: application/json' \
        -d '{"action":"sites","arguments":{}}'

The available actions are:

| Action | Purpose | Arguments |
| --- | --- | --- |
| `sites` | List configured sites and their stored-data metadata. | None. |
| `dashboard` | Return the aggregate data used by all dashboard widgets. | Optional `site`, time range, grouping, filter, and limit. |
| `pageviews` | Find pageview or event paths before deleting or merging them. | Required `search`; optional `site` and `match_case`. |
| `delete_pageviews` | Permanently delete paths, raw hits, and their aggregates. | `path_ids`; optional `site`. |
| `merge_pageviews` | Permanently merge source paths into a target path. | `path_ids`, `target_path_id`; optional `site`. |
| `import_raw` | Import historical raw hits and update all aggregates. | `hits`; optional destructive `replace`. |

When an action has an optional `site` and it is omitted, the first site in
`GOATCOUNTER_SITES` is used. Unknown sites and path IDs are rejected.

##### Sites

The `sites` result contains the configured `site`, `link_domain`, whether data
has been received, the first-hit time, and the number of stored raw pageviews.
It also reports the dashboard timezone.

##### Dashboard data

`dashboard` accepts:

- `site`: configured site name; defaults to the first site.
- `period`: `day`, `week`, `month`, `quarter`, `half-year`, `year`, or a number
  of days. The default is `week`.
- `start` and `end`: an explicit range. Both must be supplied together, as
  `YYYY-MM-DD` dates or RFC3339 timestamps. Date-only `end` values include the
  entire day in the configured timezone.
- `group`: `hour`, `day`, `week`, or `month`; defaults to `day`.
- `filter`: the same optional path-filter expression accepted by the dashboard.
- `limit`: maximum rows per ranked widget, from 1 to 1000; defaults to 100.

The result includes the resolved site, UTC start and end timestamps, grouping,
and structured data for `totalcount`, `totalpages`, `pages`, `toprefs`,
`campaigns`, `browsers`, `systems`, `locations`, `languages`, and `sizes`.
Totals include visits, raw pageviews, bounce rate, views per visit, and average
visit duration.

    curl https://stats.example.com/api \
        -H "Authorization: Bearer $GOATCOUNTER_API_TOKEN" \
        -H 'Content-Type: application/json' \
        -d '{
          "action":"dashboard",
          "arguments":{"site":"example.com","period":"month","group":"day","limit":20}
        }'

##### Managing pageviews

The `pageviews` action replaces the former “Manage pageviews” settings page.
Its required `search` value uses SQL LIKE matching: `%` matches any sequence and
`_` matches one character. Escape them with `\%` and `\_` to search for literal
characters. Matching is case-insensitive unless `match_case` is true. Results
include the path IDs required by the destructive actions.

    curl https://stats.example.com/api \
        -H "Authorization: Bearer $GOATCOUNTER_API_TOKEN" \
        -H 'Content-Type: application/json' \
        -d '{
          "action":"pageviews",
          "arguments":{"site":"example.com","search":"/old/%"}
        }'

`delete_pageviews` permanently removes every listed path and all raw and
aggregate data belonging to it:

    {"action":"delete_pageviews","arguments":{"site":"example.com","path_ids":[12,15]}}

`merge_pageviews` moves all data from the listed source path IDs into the
target, combines their aggregates, and removes the source paths. Including the
target in `path_ids` has no effect, but at least one other source is required:

    {"action":"merge_pageviews","arguments":{"site":"example.com","path_ids":[12,15],"target_path_id":4}}

Both operations are synchronous, permanent, and cannot be undone through the
API.

##### Importing raw data

`import_raw` accepts between 1 and 100,000 hits per request. Each hit supports:

| Field | Required | Meaning |
| --- | --- | --- |
| `site` | Yes | A name present in `GOATCOUNTER_SITES`. |
| `path` | Yes | Page path or event name. |
| `created_at` or `created_at_unix` | Yes | RFC3339 timestamp or Unix seconds. Timestamps more than five seconds in the future are rejected. |
| `event` | No | Whether the row is an event; defaults to false. |
| `ref` | No | Referrer value. |
| `ref_scheme` | No | `h` HTTP, `g` generated/grouped, `c` campaign, or `o` other. Defaults to `h` for a non-empty referrer and `o` otherwise. |
| `campaign` | No | Campaign name. |
| `browser`, `browser_version` | No | Parsed browser values. |
| `system`, `system_version` | No | Parsed operating-system values. |
| `location` | No | Country or subdivision code, such as `DE` or `US-CA`. |
| `language` | No | ISO 639-3 language code. |
| `width` | No | Screen width. |
| `session` | No | A 128-bit session ID encoded as 32 hexadecimal characters. Use one ID for all hits in a visit. |
| `first_visit` | No | True for the first time that session viewed this path; these rows contribute to visit aggregates. |

Raw pageviews are always stored. For meaningful visit counts, bounce rate, and
visit duration, imports should provide stable session IDs and mark
`first_visit` correctly. A zero/omitted session is accepted but cannot preserve
the original visit boundaries.

Setting `replace` to true permanently clears existing data for every site that
appears in `hits` before importing the new rows. Basic fields are validated
before clearing, but later database validation can still fail, so `replace`
must be treated as a destructive operation rather than an atomic swap.

    {
      "action": "import_raw",
      "arguments": {
        "replace": false,
        "hits": [{
          "site": "example.com",
          "path": "/docs",
          "created_at": "2026-09-12T10:30:00Z",
          "session": "00112233445566778899aabbccddeeff",
          "first_visit": true,
          "ref": "www.google.com",
          "ref_scheme": "h",
          "browser": "Firefox",
          "browser_version": "143",
          "system": "Linux",
          "language": "eng",
          "width": 1920
        }]
      }
    }

[`sample-data.sh`](sample-data.sh) is a complete bulk-import example:

    GOATCOUNTER_API_TOKEN=... ./sample-data.sh -u https://stats.example.com/api

#### MCP

Use the same `/api` URL as a remote MCP Streamable HTTP server and configure
the Bearer header in the MCP client. For clients whose configuration accepts a
URL and headers, the relevant values are:

    {
      "url": "https://stats.example.com/api",
      "headers": {
        "Authorization": "Bearer a-long-random-secret"
      }
    }

The server is stateless and exposes the six JSON actions above as MCP tools with
the same names, arguments, results, and destructive behavior. It supports MCP
protocol revision `2026-07-28`, including `server/discover`, `tools/list`,
`tools/call`, and `ping`, as well as the `initialize` /
`notifications/initialized` exchange used by older clients. Current clients
send the required `MCP-Protocol-Version`, `Mcp-Method`, and (for tool calls)
`Mcp-Name` headers automatically.

Tool results include both `structuredContent` and a JSON text content block.
Action-level failures are returned as MCP tool errors so an agent can inspect
and correct its arguments. This server has no server-to-client notifications,
subscriptions, or SSE stream; an MCP `GET` requesting `text/event-stream`
returns 405.

### Updating
There is no migration path between schema versions; start with a fresh database
after a schema change. Sites can be added, removed, or reordered because their
configured names are stored with the data.

### Building from source
You need Go 1.27 or newer, Node.js 22.12 or newer, and a C compiler.

You can build from source with:

    % git clone https://github.com/marvinrabe/goatcounter
    % cd goatcounter
    % npm ci
    % npm run build
    % go build ./cmd/goatcounter

This builds the Vite-managed frontend assets and produces a `goatcounter`
binary in the current directory.

JavaScript sources and tests live in `assets/js/`, and stylesheets in
`assets/css/`. `public/` is generated on each frontend build. Edit handwritten
static files such as `robots.txt` and `security.txt` in `assets/static/`; Vite
copies them into `public/` alongside the compiled assets, and Go embeds them
in the binary.

To build a fully statically linked binary:

    % go build -trimpath -ldflags='-s -w -extldflags=-static' \
        -tags='osusergo,netgo' \
        ./cmd/goatcounter

### Development/testing
You can start a test/development server with:

    % goatcounter serve -dev

The `-dev` flag makes some small things a bit more convenient for development:
templates and static files will be read directly from the filesystem.

See [.github/CONTRIBUTING.md](/.github/CONTRIBUTING.md) for more details on how
to run a development server, write patches, etc.
