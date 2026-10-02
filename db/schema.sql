-- GoatCounter database schema (SQLite / libSQL).
--
-- Sites are configured through GOATCOUNTER_SITES; there is intentionally no
-- sites table. The configured site name is stored as `site` on data rows.
--
-- Everything the dashboard shows is calculated from these tables at query
-- time; there are no derived aggregate tables to keep in sync, and nothing
-- is written outside of the /count request.

-- All analytics data: collected pageviews and custom events, and the history
-- migrated from Plausible. Dimension names and values follow Plausible's, so
-- both line up.
--
-- A collected row is one pageview or custom event; aggregate is ''.
--
-- A migrated row is one row of a Plausible export, with Plausible's counts
-- in the metric columns at the end. The export has a daily total for each
-- dimension separately (visits per browser per day, visits per source per
-- day, …), and aggregate is the export file the row is from. It only has the
-- dimensions of that file:
--
--   aggregate          dimensions
--   visitors           (none)
--   pages              hostname, path
--   entry_pages        path
--   exit_pages         path
--   sources            source, referrer, utm_*
--   browsers           browser, browser_version
--   operating_systems  os, os_version
--   devices            device
--   locations          country
--   custom_events      name, props ({"url": …} or {"path": …})
--   custom_props       props ({"property": "value"})
--
-- Its ts is midnight of the day in the dashboard timezone, so date ranges
-- select collected and migrated rows the same way.
create table events (
	site             text     not null,
	ts               integer  not null,                 -- Unix time, seconds.
	aggregate        text     not null default '',      -- '' for collected rows; see above.
	visitor          integer  not null default 0,       -- Hash of daily salt, site, IP, and User-Agent.
	session          integer  not null default 0,       -- Visit; ends after 30 minutes of inactivity.
	name             text     not null default 'pageview', -- "pageview", or a custom event name.
	hostname         text     not null default '',      -- Without "www.".
	path             text     not null default '',      -- URL path as sent: decoded, without query string.
	props            text     not null default '',      -- Custom event properties as a JSON object.

	source           text     not null default '',      -- "Google", "news.ycombinator.com", or utm_source.
	referrer         text     not null default '',      -- Host without "www." and path.
	utm_source       text     not null default '',
	utm_medium       text     not null default '',
	utm_campaign     text     not null default '',
	utm_content      text     not null default '',
	utm_term         text     not null default '',

	browser          text     not null default '',
	browser_version  text     not null default '',      -- major.minor
	os               text     not null default '',
	os_version       text     not null default '',
	width            integer  not null default 0,       -- Screen width in CSS pixels; 0 if unknown.
	device           text     not null default '',      -- Migrated only: Mobile, Tablet, Laptop, Desktop.

	country          text     not null default '',      -- ISO 3166-1 alpha-2.
	language         text     not null default '',      -- ISO 639-3.

	-- Plausible's counts for migrated rows; 0 for collected rows.
	visitors                   integer  not null default 0,
	visits                     integer  not null default 0,
	pageviews                  integer  not null default 0,
	bounces                    integer  not null default 0,
	visit_duration             integer  not null default 0,
	events                     integer  not null default 0,
	entrances                  integer  not null default 0,
	exits                      integer  not null default 0,
	total_scroll_depth         integer  not null default 0,
	total_scroll_depth_visits  integer  not null default 0,
	total_time_on_page         integer  not null default 0,
	total_time_on_page_visits  integer  not null default 0
) strict;
-- Covers the dashboard's range scans of collected and migrated rows, and the
-- 30-minute session lookup done for every collected pageview.
create index "events#site#aggregate#ts" on events(site, aggregate, ts, visitor, session);

-- Daily random salts for visitor hashes. Salts older than yesterday are
-- deleted, after which visitor hashes can no longer be linked to an IP.
create table salts (
	day   text  not null primary key,
	salt  blob  not null
) strict;
