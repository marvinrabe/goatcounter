package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"zgo.at/zli"
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

func runHealthcheck(f zli.Flags, ready chan<- struct{}, stop chan struct{}) error {
	url := f.String("", "url")
	timeout := f.Int(3, "timeout")
	if err := f.Parse(); err != nil {
		return err
	}

	target := url.String()
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
	client := &http.Client{Timeout: time.Duration(timeout.Int()) * time.Second}
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
