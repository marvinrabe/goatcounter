package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

const cmdHealthcheck = `
Perform a health check against a running GoatCounter instance: request /status
and exit 0 if healthy, 1 if not. Intended for Docker HEALTHCHECK (the default
image is "from scratch" and has no shell, curl, or wget).

Flags:

  -url         URL to check. Default: derive from GOATCOUNTER_LISTEN and
               GOATCOUNTER_BASE_PATH (http://localhost:8080/status when unset).
  -timeout     Timeout for the request, in seconds. Default: 3
`

func runHealthcheck(args []string, ready chan<- struct{}, stop chan struct{}) error {
	f := newFlags("runHealthcheck")
	url := f.String("url", "", "")
	timeout := f.Int("timeout", 3, "")
	if err := parseFlags(f, args, false, false); err != nil {
		return err
	}

	target := *url
	if target == "" {
		listen := os.Getenv("GOATCOUNTER_LISTEN")
		if listen == "" {
			listen = ":8080"
		}
		host, port, err := net.SplitHostPort(listen)
		if err != nil {
			return fmt.Errorf("healthcheck address: %w", err)
		}
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		target = "http://" + net.JoinHostPort(host, port) + os.Getenv("GOATCOUNTER_BASE_PATH") + "/status"
	}
	client := &http.Client{Timeout: time.Duration(*timeout) * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		return fmt.Errorf("healthcheck failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck failed: status %d", resp.StatusCode)
	}
	return nil
}
