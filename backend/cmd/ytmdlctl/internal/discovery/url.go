// Package discovery provides deployment discovery, health checks, and container inspection.
package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"ytdm/backend/cmd/ytmdlctl/internal/engine"
)

// NormalizeURL validates and normalizes a candidate URL to http/https and local loopback if wildcard.
func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("empty URL")
	}

	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme %q (only http and https supported)", parsed.Scheme)
	}

	host := parsed.Hostname()
	port := parsed.Port()

	// Normalize wildcard addresses to local loopback
	if host == "0.0.0.0" || host == "::" || host == "" {
		host = "127.0.0.1"
	}

	if port != "" {
		parsed.Host = net.JoinHostPort(host, port)
	} else {
		parsed.Host = host
	}

	return strings.TrimRight(parsed.String(), "/"), nil
}

// ResolveBaseURLOptions configures the base URL resolution hierarchy.
type ResolveBaseURLOptions struct {
	ExplicitURL  string
	PersistedURL string
	Engine       engine.Engine
	ProjectDir   string
	ComposeFile  string
	EnvVars      map[string]string
}

// ResolveBaseURL discovers the reachable base URL following the strict priority hierarchy.
func ResolveBaseURL(ctx context.Context, opts ResolveBaseURLOptions) (string, error) {
	// 1. Explicit CLI flag
	if opts.ExplicitURL != "" {
		return NormalizeURL(opts.ExplicitURL)
	}

	// 2. Persisted config in .ytmdl/config.json
	if opts.PersistedURL != "" {
		return NormalizeURL(opts.PersistedURL)
	}

	// 3. Compose port discovery on frontend service (port 8080)
	if opts.Engine != nil && opts.ComposeFile != "" {
		portOutput, err := opts.Engine.Port(ctx, opts.ProjectDir, opts.ComposeFile, "frontend", 8080)
		if err == nil && portOutput != "" {
			norm, nErr := NormalizeURL(portOutput)
			if nErr == nil {
				return norm, nil
			}
		}
	}

	// 4. .env YTMDL_HOST_PORT fallback
	if opts.EnvVars != nil {
		if hostPort := strings.TrimSpace(opts.EnvVars["YTMDL_HOST_PORT"]); hostPort != "" {
			if _, pErr := strconv.Atoi(hostPort); pErr == nil {
				return fmt.Sprintf("http://127.0.0.1:%s", hostPort), nil
			}
		}
	}

	// 5. Default port 8080 fallback
	return "http://127.0.0.1:8080", nil
}
