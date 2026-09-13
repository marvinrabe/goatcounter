package main

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

func printHelp(t string) {
	fmt.Fprintln(stdout, strings.TrimSpace(t))
}

func cmdHelp(args []string, ready chan<- struct{}, stop chan struct{}) error {
	defer func() { ready <- struct{}{} }()

	// Help accepts topic names and ignores flag switches.
	var topics []string
	for _, a := range args {
		if len(a) == 0 || a[0] == '-' {
			continue
		}
		if a == "all" {
			topics = []string{"help", "serve", "healthcheck", "geodb-update", "listen", "debug"}
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
			return fmt.Errorf("no help topic for %q", topics[0])
		}
		printHelp(text)
	default:
		for _, t := range topics {
			text, ok := usage[t]
			if !ok {
				return fmt.Errorf("no help topic for %q", t)
			}

			head := fmt.Sprintf("─── Help for %q ", t)
			fmt.Fprintf(stdout, "%s%s\n\n",
				head,
				strings.Repeat("─", 80-utf8.RuneCountInString(head)))
			printHelp(text)
			fmt.Fprintln(stdout, "")
		}
	}
	return nil
}

var usage = map[string]string{
	"":             usageTop,
	"help":         usageHelp,
	"serve":        usageServe,
	"listen":       helpListen,
	"debug":        helpDebug,
	"healthcheck":  cmdHealthcheck,
	"geodb-update": usageGeoDB,
}

const usageTop = `Usage: goatcounter [command] [flags]

GoatCounter is a web analytics platform. https://github.com/arp242/goatcounter
Use "help <topic>" or "cmd -h" for more details for a command or topic.

Commands:
  help         Show help; use "help <topic>" or "help all" for more details.
  serve        Start HTTP server.
  geodb-update Download a GeoIP database as a one-off operation.
  healthcheck  Check a running instance is healthy; for Docker HEALTHCHECK.

Extra help topics:
  listen       Detailed documentation on -listen flag.
  debug        Debug logging options.
`

const usageHelp = `
Show help; use "help commands" to dispay detailed help for a command, or "help
all" to display everything.
`

const helpListen = `
Use -listen or GOATCOUNTER_LISTEN to set the server's address and port. The
default is :8080; there is no separate public-port setting. For example:

    -listen localhost:8081     Listen on localhost:8081
    -listen :8081              Listen on :8081 for all addresses

GoatCounter always serves plain HTTP; put a reverse proxy in front of it if you
want TLS.
`

const helpDebug = `
Use -debug to enable debug logs, including HTTP requests (except /count and
/robots.txt). Use -debug-sql to log SQL queries independently.

Logs use Go's standard slog JSON format, or text format with -dev. The module
field identifies the part of the application that produced the message.
`
