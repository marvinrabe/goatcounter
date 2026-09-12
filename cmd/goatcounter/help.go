package main

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"zgo.at/errors"
	"zgo.at/zli"
)

func printHelp(t string) {
	fmt.Fprint(zli.Stdout, zli.Usage(zli.UsageTrim|zli.UsageHeaders, t))
}

func cmdHelp(f zli.Flags, ready chan<- struct{}, stop chan struct{}) error {
	defer func() { ready <- struct{}{} }()

	zli.WantColor = true

	// Don't parse any flags, just grep out non-flags and print help for those.
	// zli.Flags stops at an unknown flag, so "-site 1" tries to load the help
	// for "1"; that's the price for being able to append "-h" to any command.
	var topics []string
	for _, a := range f.Args {
		if len(a) == 0 || a[0] == '-' {
			continue
		}
		if a == "all" {
			topics = []string{"help", "version", "serve", "healthcheck", "listen", "debug"}
			break
		}
		topics = append(topics, strings.ToLower(a))
	}

	switch len(topics) {
	case 0:
		printHelp(usage[""])
	case 1:
		text, ok := usage[topics[0]]
		if !ok {
			return errors.Errorf("no help topic for %q", topics[0])
		}
		printHelp(text)
	default:
		for _, t := range topics {
			text, ok := usage[t]
			if !ok {
				return errors.Errorf("no help topic for %q", t)
			}

			head := fmt.Sprintf("─── Help for %q ", t)
			fmt.Fprintf(zli.Stdout, "%s%s\n\n",
				zli.Colorize(head, zli.Bold),
				strings.Repeat("─", 80-utf8.RuneCountInString(head)))
			printHelp(text)
			fmt.Fprintln(zli.Stdout, "")
		}
	}
	return nil
}

var usage = map[string]string{
	"":            usageTop,
	"help":        usageHelp,
	"serve":       usageServe,
	"listen":      helpListen,
	"debug":       helpDebug,
	"healthcheck": cmdHealthcheck,

	"version": `
Show version and build information. This is printed as key=value, separated by
semicolons.

Flags:

  -json        Output version as JSON.
`,
}

const usageTop = `Usage: goatcounter [command] [flags]

GoatCounter is a web analytics platform. https://github.com/arp242/goatcounter
Use "help <topic>" or "cmd -h" for more details for a command or topic.

Commands:
  help         Show help; use "help <topic>" or "help all" for more details.
  version      Show version and build information and exit.
  serve        Start HTTP server.
  healthcheck  Check a running instance is healthy; for Docker HEALTHCHECK.

Extra help topics:
  listen       Detailed documentation on -listen flag.
  debug        List of modules accepted by the -debug flag.
`

const usageHelp = `
Show help; use "help commands" to dispay detailed help for a command, or "help
all" to display everything.
`

const helpListen = `
You can change the main port GoatCounter listens on with the -listen flag. This
works like most applications, for example:

    -listen localhost:8081     Listen on localhost:8081
    -listen :8081              Listen on :8081 for all addresses

GoatCounter always serves plain HTTP; put a reverse proxy in front of it if you
want TLS.
`

const helpDebug = `
List of debug modules for the -debug flag; you can add multiple separated by
commas.

    all            Show debug logs for all of the below
    cli-trace      Show stack traces in errors on the CLI
    cron           Background "cron" jobs, including vacuuming old pageviews
    dashboard      Dashboard view
    geo            Loading of the GeoIP database
    memstore       Storing of pageviews in the database
    refspam        Pageviews blocked due to being in the refspam list
    req            HTTP requests (all except /count and /robots.txt)
    session        Internal "session" generation to track visitors
    sql-query      Log all SQL queries
    sql-result     Log all SQL queries with the data they're returning.

You can also disable a module by prefixing it with "-", for example
"-debug=all,-sql-query" to debug everything except SQL queries.
`
