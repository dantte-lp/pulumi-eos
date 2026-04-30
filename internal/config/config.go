// Package config holds the pulumi-eos provider-level configuration type and
// client factories. It is consumed both by `internal/provider` (which builds
// the inferred provider) and by every `internal/resources/<area>` package
// (which retrieves the active config via FromContext).
package config

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/pulumi/pulumi-go-provider/infer"
)

// Defaults for provider configuration. Overridable per-stack via Pulumi config
// or via environment variables (see Config.fromEnv).
const (
	defaultTransport      = "eapi"
	defaultRequestTimeout = 30 * time.Second
	defaultSessionPrefix  = "pulumi-"
	defaultRetryAttempts  = 5
	defaultRetryBaseDelay = 1 * time.Second
	defaultRetryMaxDelay  = 30 * time.Second
)

// Sentinel errors returned by Configure.
var (
	ErrNoEndpoint        = errors.New("at least one of eosUrl or cvpUrl must be set")
	ErrUnsupportedTransp = errors.New("eosTransport must be 'eapi' or 'gnmi'")
)

// Transport is the on-box management plane chosen for EOS resources.
type Transport string

const (
	TransportEAPI Transport = "eapi"
	TransportGNMI Transport = "gnmi"
)

// Config holds the provider-level configuration surfaced to Pulumi programs.
//
// Fields are split between the EOS (on-box) and CVP (fleet) planes. A given
// program may populate either or both; resources whose family begins with
// "eos:cvp:" require the CVP fields, all other "eos:*" resources require the
// EOS fields.
type Config struct {
	// EOS — on-box management.
	EOSURL            string  `pulumi:"eosUrl,optional"`
	EOSUsername       string  `pulumi:"eosUsername,optional"`
	EOSPassword       *string `provider:"secret"                   pulumi:"eosPassword,optional"`
	EOSTLSCACert      *string `provider:"secret"                   pulumi:"eosTlsCaCert,optional"`
	EOSTLSClientCert  *string `provider:"secret"                   pulumi:"eosTlsClientCert,optional"`
	EOSTLSClientKey   *string `provider:"secret"                   pulumi:"eosTlsClientKey,optional"`
	EOSInsecure       *bool   `pulumi:"eosInsecure,optional"`
	EOSTransport      *string `pulumi:"eosTransport,optional"`
	EOSRequestTimeout *string `pulumi:"eosRequestTimeout,optional"`
	EOSSessionPrefix  *string `pulumi:"eosSessionPrefix,optional"`

	// CVP / CVaaS — fleet management.
	CVPURL    string  `pulumi:"cvpUrl,optional"`
	CVPToken  *string `provider:"secret"        pulumi:"cvpToken,optional"`
	CVPCACert *string `provider:"secret"        pulumi:"cvpCaCert,optional"`

	// Retry policy.
	RetryMaxAttempts *int    `pulumi:"retryMaxAttempts,optional"`
	RetryBaseDelay   *string `pulumi:"retryBaseDelay,optional"`
	RetryMaxDelay    *string `pulumi:"retryMaxDelay,optional"`
}

// Annotate registers human-readable descriptions used by Pulumi schema
// generation and by `pulumi config set --help`.
func (c *Config) Annotate(a infer.Annotator) {
	a.Describe(&c, "Provider configuration for pulumi-eos.")
	a.Describe(&c.EOSURL, "Base URL of the EOS device (eAPI HTTPS) or host:port for gNMI. Example: https://leaf-01.example.net.")
	a.Describe(&c.EOSUsername, "EOS AAA username for eAPI basic auth.")
	a.Describe(&c.EOSPassword, "EOS AAA password.")
	a.Describe(&c.EOSTLSCACert, "PEM-encoded CA certificate or filesystem path for EOS server verification.")
	a.Describe(&c.EOSTLSClientCert, "PEM-encoded client certificate or filesystem path for mTLS.")
	a.Describe(&c.EOSTLSClientKey, "PEM-encoded client private key or filesystem path for mTLS.")
	a.Describe(&c.EOSInsecure, "Disable TLS verification (development only).")
	a.Describe(&c.EOSTransport, "On-box transport: 'eapi' or 'gnmi'. Defaults to 'eapi'.")
	a.Describe(&c.EOSRequestTimeout, "Per-request timeout (Go duration). Defaults to 30s.")
	a.Describe(&c.EOSSessionPrefix, "Prefix for EOS configuration session names. Defaults to 'pulumi-'.")
	a.Describe(&c.CVPURL, "CVP / CVaaS base URL. Example: https://www.arista.io.")
	a.Describe(&c.CVPToken, "CVP / CVaaS service-account bearer token.")
	a.Describe(&c.CVPCACert, "PEM-encoded CA certificate or filesystem path for CVP TLS.")
	a.Describe(&c.RetryMaxAttempts, "Maximum retry attempts for transient errors. Defaults to 5.")
	a.Describe(&c.RetryBaseDelay, "Initial retry delay (Go duration). Defaults to 1s.")
	a.Describe(&c.RetryMaxDelay, "Maximum retry delay (Go duration). Defaults to 30s.")
}

// Configure validates the populated config and prepares derived state.
//
// It is invoked once by the Pulumi engine before any resource operation.
// Failures here surface to the user as `pulumi config` errors.
func (c *Config) Configure(_ context.Context) error {
	if c.EOSURL != "" {
		if _, err := url.Parse(c.EOSURL); err != nil {
			return fmt.Errorf("eosUrl is not a valid URL: %w", err)
		}
	}
	if c.CVPURL != "" {
		if _, err := url.Parse(c.CVPURL); err != nil {
			return fmt.Errorf("cvpUrl is not a valid URL: %w", err)
		}
	}
	if c.EOSURL == "" && c.CVPURL == "" {
		return ErrNoEndpoint
	}

	if t := c.transport(); t != TransportEAPI && t != TransportGNMI {
		return fmt.Errorf("%w, got %q", ErrUnsupportedTransp, t)
	}
	if _, err := c.requestTimeout(); err != nil {
		return fmt.Errorf("eosRequestTimeout: %w", err)
	}
	if _, err := c.retryBase(); err != nil {
		return fmt.Errorf("retryBaseDelay: %w", err)
	}
	if _, err := c.retryMax(); err != nil {
		return fmt.Errorf("retryMaxDelay: %w", err)
	}
	return nil
}

// SessionPrefix returns the EOS configuration session name prefix.
func (c *Config) SessionPrefix() string {
	if c.EOSSessionPrefix == nil || *c.EOSSessionPrefix == "" {
		return defaultSessionPrefix
	}
	return *c.EOSSessionPrefix
}

// RetryAttempts returns the effective retry-attempt count.
func (c *Config) RetryAttempts() int {
	if c.RetryMaxAttempts == nil || *c.RetryMaxAttempts < 1 {
		return defaultRetryAttempts
	}
	return *c.RetryMaxAttempts
}

// HasEOS reports whether on-box EOS configuration is populated.
func (c *Config) HasEOS() bool { return c.EOSURL != "" }

// HasCVP reports whether CVP / CVaaS configuration is populated.
func (c *Config) HasCVP() bool { return c.CVPURL != "" }

// transport returns the effective transport, falling back to the default.
func (c *Config) transport() Transport {
	if c.EOSTransport == nil || *c.EOSTransport == "" {
		return Transport(defaultTransport)
	}
	return Transport(strings.ToLower(*c.EOSTransport))
}

func (c *Config) requestTimeout() (time.Duration, error) {
	if c.EOSRequestTimeout == nil || *c.EOSRequestTimeout == "" {
		return defaultRequestTimeout, nil
	}
	return time.ParseDuration(*c.EOSRequestTimeout)
}

func (c *Config) retryBase() (time.Duration, error) {
	if c.RetryBaseDelay == nil || *c.RetryBaseDelay == "" {
		return defaultRetryBaseDelay, nil
	}
	return time.ParseDuration(*c.RetryBaseDelay)
}

func (c *Config) retryMax() (time.Duration, error) {
	if c.RetryMaxDelay == nil || *c.RetryMaxDelay == "" {
		return defaultRetryMaxDelay, nil
	}
	return time.ParseDuration(*c.RetryMaxDelay)
}
