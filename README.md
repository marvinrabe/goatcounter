This is a heavily trimmed-down fork of [GoatCounter][www] for self-hosting a
single site with the lowest possible CPU and RAM usage. What's left is the
`count.js` tracking script, the collection endpoint, and the dashboard; most of
what makes upstream a hosted, multi-tenant, multi-language product is gone.

Collecting data:

- No API: the `/api/v0` endpoints and API tokens are gone, and so is the
  `api_token` table.
- No log-file import (`goatcounter import`) and no importing from another
  GoatCounter instance.
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
- No multi-site/SaaS: one site, served on whatever domain you point at it.
  There is no `sites` table, no vhost/CNAME routing, and no `site_id` column on
  any table.
- No email at all: no SMTP, no email reports, no password resets, no email
  verification, no "email" debug pages.
- English only: the `i18n/` translations, the translation UI, and the
  per-user locale are gone. Dates, numbers, and times use international formats
  (ISO dates, 24-hour clock, thin-space thousands separator).
- Only four commands: `serve`, `db`, `help`, and `version`. The `import`,
  `monitor`, and `dashboard` commands are gone, as are the `db` subcommands for
  sites and API tokens.
- No runtime metrics collection and no admin ("bosmang") pages for cache,
  background jobs, GeoIP, and metrics.

Web interface:

- Every user is an admin; there are no access levels and no two-factor auth.
  Authentication is HTTP basic auth: the username is ignored and the password
  is checked against the users' bcrypt hashes.
- No user settings page. The dashboard layout is fixed (totals first), the
  timezone comes from the `TZ` environment variable, and everything else that
  was a user preference is now a site setting.
- No signup, no user management UI, no site deletion, no changing the site
  code. Settings are two pages: the site settings and managing pageviews.
- No marketing website (home, "why", contact, contribute, design) and no
  in-app help pages.
- No dark theme, no footer menu, and no custom date picker — the browser's
  native date input is used instead.
- No third-party front-end code: jQuery, dragula, and pikaday are gone, as are
  the bundled Lato webfonts (a system font stack is used).
- No dashboard websocket loader; widgets are rendered with the page and only
  paging and drill-downs fetch more.

Users are managed with `goatcounter db create|update|delete user`; there is no
in-app user management.

The dashboard is displayed in the timezone from the `TZ` environment variable
(e.g. `TZ=Europe/Berlin`), for everyone; it defaults to UTC.

[www]: https://www.goatcounter.com


Features
--------
- **Privacy-aware**; doesn’t track users with unique identifiers and doesn't
  need a GDPR notice. Fine-grained **control over which data is collected**.

- **Lightweight** and **fast**; adds just ~3.5K of extra data to your site.

- Identify **unique visits** without cookies using a non-identifiable hash.

- Keeps useful statistics such as **browser** information, **location**,
  **language**, and **screen size**. Keep track of **referring sites** and
  **campaigns**.

- **Own your data**; everything is in a single SQLite database.

- Integrate on your site with just a **single script tag**:

      <script data-goatcounter="https://stats.example.com/count"
              async src="//stats.example.com/count.js"></script>


Self-hosting GoatCounter
------------------------
The only dependency is somewhere to store a SQLite database file. Alternatively
you can use Docker, as documented in the section below.

### Running
You can start a server with:

    % goatcounter serve

This will start a server on `*:8080`. The default is to use an SQLite database
at `./goatcounter-data/db.sqlite3`, which will be created if it doesn't exist
yet.

The site itself is created automatically on first run. To create a user to log
in with:

    % goatcounter db create user -email=me@example.com -password=secret

You must also pass the `-db` flag here if you use something other than the
default. Log in on the web interface with HTTP basic auth; any username works,
the password is what you set above.

GoatCounter serves plain HTTP; put a reverse proxy (nginx, Caddy, …) in front
of it for TLS.

### Running with Docker
There is no published image for this fork; the images on DockerHub are
upstream's, with everything listed above still in them. Build it yourself:

    % docker build -t goatcounter .
    % docker run \
        -p 8080:8080 \
        -v goatcounter-data:/home/goatcounter/goatcounter-data \
        goatcounter

This uses a named volume, which is recommended as this stores the SQLite
database and anonymous volumes can be easy to accidentally delete.

To create the first user:

    % docker exec -it [..] goatcounter db create user \
        -email=me@example.com -password=secret

`compose.yaml` has a ready-to-run example, including the memory limits and
`GOGC`/`GOMEMLIMIT` settings this fork is tuned for.

### Management
A status URL is available at `/status`, which can be used for health monitors.

### Updating
Databases created before the multi-site removal are not upgradable; start with a
fresh database.

The migration machinery is still there for future schema changes: use
`goatcounter serve -automigrate` to run all pending migrations on startup, or
`goatcounter db migrate all` to run them manually. `goatcounter db migrate
pending` lists pending migrations and `goatcounter db migrate list` shows all of
them.

### Building from source
You need Go 1.27 or newer, Node.js 22.12 or newer, and a C compiler
(for SQLite).

You can build from source with:

    % git clone https://github.com/marvinrabe/goatcounter
    % cd goatcounter
    % npm ci
    % npm run build
    % go build ./cmd/goatcounter

This builds the Vite-managed frontend assets and produces a `goatcounter`
binary in the current directory.

To build a fully statically linked binary:

    % go build -trimpath -ldflags='-s -w -extldflags=-static' \
        -tags='osusergo,netgo,sqlite_omit_load_extension' \
        ./cmd/goatcounter

### Development/testing
You can start a test/development server with:

    % goatcounter serve -dev

The `-dev` flag makes some small things a bit more convenient for development:
templates and static files will be read directly from the filesystem.

See [.github/CONTRIBUTING.md](/.github/CONTRIBUTING.md) for more details on how
to run a development server, write patches, etc.
