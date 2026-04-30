// Package cvp wraps github.com/aristanetworks/cloudvision-go for the subset
// of CVP / CVaaS Resource APIs used by pulumi-eos.
//
// The wrapper centralises authentication (service-account bearer token over
// mTLS-capable gRPC) and provides typed entry points per resource family
// (Workspaces, Studios, Configlets, Change Control, Tags, Inventory,
// Service Accounts). Concrete clients land in S8 — at S4 only Connect lives.
package cvp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// DefaultGRPCPort is CVP's gRPC API gateway port.
const DefaultGRPCPort = 443

// Sentinel errors returned by package cvp.
var (
	ErrURLRequired  = errors.New("cvp: url is required")
	ErrTokenMissing = errors.New("cvp: token is required")
	ErrInvalidPEM   = errors.New("cvp: caCert is not a valid PEM bundle")
	ErrMissingHost  = errors.New("cvp: url is missing a host component")
)

// Config is the connection-level configuration for CVP / CVaaS.
//
// TLS is always verified. To accept a self-signed CVP deployment, supply the
// internal CA via CACertPEM rather than disabling verification.
type Config struct {
	// URL is the CVP base URL (e.g. https://www.arista.io).
	URL string
	// Token is the service-account bearer token. Treated as a secret.
	Token string
	// CACertPEM optionally pins the CA bundle. If empty the host's roots are
	// used.
	CACertPEM []byte
}

func (c Config) validate() error {
	if c.URL == "" {
		return ErrURLRequired
	}
	if c.Token == "" {
		return ErrTokenMissing
	}
	if _, err := url.Parse(c.URL); err != nil {
		return fmt.Errorf("cvp: url is not parseable: %w", err)
	}
	return nil
}

// Client is a thin handle over a single CVP / CVaaS connection.
//
// It carries the underlying *grpc.ClientConn so per-service helpers added in
// S8 can share the same multiplexed channel.
type Client struct {
	cfg  Config
	conn *grpc.ClientConn
}

// New dials CVP and returns a connected *Client.
//
// Cancellation: respects ctx during the dial; ctx cancellation after the dial
// completes does NOT close the connection — call Close explicitly.
func New(ctx context.Context, cfg Config) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	tlsCfg, err := buildTLS(cfg)
	if err != nil {
		return nil, err
	}

	target, err := target(cfg.URL)
	if err != nil {
		return nil, err
	}

	conn, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)),
		grpc.WithPerRPCCredentials(bearerCreds{token: cfg.Token}),
	)
	if err != nil {
		return nil, fmt.Errorf("cvp: dial %s: %w", target, err)
	}
	if err := ctx.Err(); err != nil {
		if cerr := conn.Close(); cerr != nil {
			return nil, fmt.Errorf("cvp: ctx %w; close: %w", err, cerr)
		}
		return nil, err
	}
	return &Client{cfg: cfg, conn: conn}, nil
}

// Conn returns the underlying gRPC connection. Per-service helpers in
// internal/client/cvp/<service>/ wrap this connection.
func (c *Client) Conn() *grpc.ClientConn { return c.conn }

// Close terminates the gRPC connection.
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// LoadCACertFile reads a CA bundle from disk and returns the PEM bytes.
//
// Provided as a convenience for callers that store cvpCaCert as a path
// rather than inline PEM.
func LoadCACertFile(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	return os.ReadFile(path)
}

// --- internals --------------------------------------------------------------

func buildTLS(cfg Config) (*tls.Config, error) {
	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: false, // explicit: TLS verification is mandatory.
	}
	if len(cfg.CACertPEM) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(cfg.CACertPEM) {
			return nil, ErrInvalidPEM
		}
		tlsCfg.RootCAs = pool
	}
	return tlsCfg, nil
}

func target(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("cvp: parse url: %w", err)
	}
	host := u.Host
	if host == "" {
		host = u.Path
	}
	if host == "" {
		return "", ErrMissingHost
	}
	if _, _, ok := splitPort(host); !ok {
		host = fmt.Sprintf("%s:%d", host, DefaultGRPCPort)
	}
	return host, nil
}

func splitPort(host string) (string, string, bool) {
	for i := len(host) - 1; i >= 0 && host[i] != ']'; i-- {
		if host[i] == ':' {
			return host[:i], host[i+1:], true
		}
	}
	return host, "", false
}

// bearerCreds attaches `Authorization: Bearer <token>` to every RPC.
type bearerCreds struct {
	token string
}

func (b bearerCreds) GetRequestMetadata(_ context.Context, _ ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + b.token}, nil
}

// RequireTransportSecurity always requires TLS — the bearer token is sensitive
// material and MUST never traverse cleartext transport.
func (b bearerCreds) RequireTransportSecurity() bool { return true }
