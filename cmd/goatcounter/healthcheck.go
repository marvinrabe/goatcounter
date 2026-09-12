package main

import (
	"fmt"
	"net/http"
	"time"

	"zgo.at/zli"
)

const cmdHealthcheck = `
Perform a health check against a running GoatCounter instance: request /status
and exit 0 if healthy, 1 if not. Intended for Docker HEALTHCHECK (the default
image is "from scratch" and has no shell, curl, or wget).

Flags:

  -url         URL to check. Default: http://localhost:8080/status
  -timeout     Timeout for the request, in seconds. Default: 3
`

func runHealthcheck(f zli.Flags, ready chan<- struct{}, stop chan struct{}) error {
	url := f.String("http://localhost:8080/status", "url")
	timeout := f.Int(3, "timeout")
	if err := f.Parse(); err != nil {
		return err
	}

	client := &http.Client{Timeout: time.Duration(timeout.Int()) * time.Second}
	resp, err := client.Get(url.String())
	if err != nil {
		return fmt.Errorf("healthcheck failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck failed: status %d", resp.StatusCode)
	}
	return nil
}
