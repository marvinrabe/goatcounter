You can get in touch on GitHub issues if you have any questions or problems.

Don't be afraid to just ask if you're struggling with something. Chances are
that I can quickly give you an answer or point you in the right direction by
spending just a few minutes.

Running
-------
Install the frontend dependencies once, then run Vite and GoatCounter in
separate terminals:

    % npm ci
    % npm run dev

    % goatcounter serve -dev

The `-dev` flag makes some small things a bit more convenient for development:
the application will automatically restart on recompiles, templates and built
static files will be read directly from the filesystem, and a few other minor
changes. Vite rebuilds `public/` when files in `assets/` change.

General notes
-------------
- It's probably best to create an issue first for non-trivial patches. This
  might prevent you from wasting time on a wrong approach, or from working on
  something that will never be merged.

  - I'm pretty wary of introducing dependencies, especially if they come with
    large dependency trees. So it's probably best to communicate in the issue if
    you're planning to do that.

- Use `-debug=<mod>` to enable debug logs for specific modules, or `-debug=all`
  to enable it for all modules.

- Automatic reload is managed with github.com/teamwork/reload. Basically it will
  restart on recompile, and reload templates once they change (no restart
  required).

- Tests can be run with `go test ./...`; run `npm run build` first after
  changing frontend assets.

- Keep lines under 80 characters if possible, but don't bend over backwards to
  do so: it's usually okay for a function definition or call to be 90 or even
  100 characters. Comments should pretty much always be wrapped to 80 characters
  though.

Code design
-----------
Some notes about the code; most of it should – hopefully – be fairly
straightforward

- ./cmd/goatcounter/ is the `main` package which starts everything.

- The "models" are contained in ./site.go, ./hit.go, etc. [site.go](./site.go)
  is probably the best place to look at to get an overview of the patterns used.
  Most of this is fairly straight-forward and uncomplicated.

- HTTP handlers go in ./handlers/

- The `/count` endpoint to records pageviews is dealt different than most other
  requests: instead of persisting to the DB immediately it's added to memstore
  first. The cron package will persist that to DB every 10 seconds, which also
  regenerates various cached stats.

- Hits ("pageviews") are stored in the `hits` table with minimal processing; for
  the most part, this table isn't queried directly for reasons of performance.
  When inserting new rows in the table the various `*_stats` and `*_count`
  tables are updated which contain a more efficient aggregation of the data
  (`hit_stats`, browser_stats`, etc.) This is done through the `cron` package.

- Templates live in `/tpl`, and are standard Go templates. The Go template
  library is a bit idiosyncratic, but once you "get" it they're quite pleasant
  to work with (I can't find a good/gentle "getting started with Go templates"
  introduction, let me know if you know of one; but just ask if you're
  struggling with this).

- The frontend is in /public. It's all simple basic CSS with simple jQuery-based
  JavaScript.
