package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

// healthcheck = probe for distroless image (no curl). exit 0 if 2xx.
func healthcheck(ctx context.Context, args []string) error {
	target := "http://127.0.0.1:8080/readyz"
	if v := os.Getenv("CRAWLER_HEALTHCHECK_URL"); v != "" {
		target = v
	}
	if len(args) > 0 {
		target = args[0]
	}
	if err := loopbackOnly(target); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil) //nolint:gosec // G704: loopbackOnly above

	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // G704: loopbackOnly above
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("healthcheck: status %d", resp.StatusCode)
	}
	return nil
}

// loopbackOnly: probe never leaves the pod/container. no ssrf vector.
func loopbackOnly(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("healthcheck: bad url %q", raw)
	}
	host := u.Hostname()
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("healthcheck: only loopback targets allowed, got %q", host)
}
