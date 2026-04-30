package config

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
)

// Sentinel errors returned by client factories.
var (
	ErrEOSNotConfigured  = errors.New("provider eosUrl not configured")
	ErrCVPNotConfigured  = errors.New("provider cvpUrl not configured")
	ErrUnsupportedScheme = errors.New("eosUrl must be http:// or https://")
)

// EAPIClient builds an eAPI client from the active provider configuration,
// optionally overriding host / username / password (e.g. when a per-resource
// `host` field is set).
//
// The function is exposed to resource packages (internal/resources/...) so
// each resource can derive a client without re-implementing config glue.
func (c *Config) EAPIClient(ctx context.Context, hostOverride, userOverride, passOverride *string) (*eapi.Client, error) {
	if c == nil {
		return nil, ErrEOSNotConfigured
	}
	ep, err := splitEOSURL(c.EOSURL, hostOverride)
	if err != nil {
		return nil, err
	}
	if ep.Host == "" {
		return nil, ErrEOSNotConfigured
	}

	username := c.EOSUsername
	if userOverride != nil && *userOverride != "" {
		username = *userOverride
	}
	password := ""
	if c.EOSPassword != nil {
		password = *c.EOSPassword
	}
	if passOverride != nil && *passOverride != "" {
		password = *passOverride
	}

	timeout, err := c.requestTimeout()
	if err != nil {
		return nil, fmt.Errorf("requestTimeout: %w", err)
	}

	return eapi.New(ctx, eapi.Config{
		Host:     ep.Host,
		Port:     ep.Port,
		Username: username,
		Password: password,
		Timeout:  timeout,
		UseHTTPS: ep.Scheme == "https",
	})
}

// endpoint is the parsed result of merging the provider-level eosUrl with an
// optional per-resource override.
type endpoint struct {
	Host   string
	Port   int
	Scheme string
}

// splitEOSURL parses the provider-level eosUrl plus an optional per-resource
// host override. The override may be a bare hostname (uses provider scheme /
// port) or a full URL.
func splitEOSURL(provURL string, override *string) (endpoint, error) {
	out := endpoint{Scheme: "https"}

	if override != nil && *override != "" {
		ov := *override
		if strings.Contains(ov, "://") {
			u, parseErr := url.Parse(ov)
			if parseErr != nil {
				return endpoint{}, fmt.Errorf("host override %q: %w", ov, parseErr)
			}
			out.Host, out.Port = hostPort(u)
			out.Scheme = u.Scheme
			return out, nil
		}
		out.Host = ov
	}

	if provURL == "" {
		if out.Host != "" {
			return out, nil
		}
		return endpoint{}, ErrEOSNotConfigured
	}

	u, perr := url.Parse(provURL)
	if perr != nil {
		return endpoint{}, fmt.Errorf("eosUrl %q: %w", provURL, perr)
	}
	if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "" {
		return endpoint{}, ErrUnsupportedScheme
	}
	if u.Scheme != "" {
		out.Scheme = u.Scheme
	}
	if out.Host == "" {
		out.Host, out.Port = hostPort(u)
	} else if out.Port == 0 {
		_, out.Port = hostPort(u)
	}
	return out, nil
}

func hostPort(u *url.URL) (string, int) {
	host := u.Hostname()
	port := 0
	if p := u.Port(); p != "" {
		if v, err := strconv.Atoi(p); err == nil {
			port = v
		}
	}
	return host, port
}

// FromContext retrieves the active provider Config from the context. It is
// the canonical accessor used by resource implementations.
func FromContext(ctx context.Context) *Config {
	cfg := infer.GetConfig[Config](ctx)
	return &cfg
}
