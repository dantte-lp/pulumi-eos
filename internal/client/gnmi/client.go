// Package gnmi provides a minimum gRPC Network Management Interface
// (gNMI) client tailored to pulumi-eos. The surface is intentionally
// narrow: Capabilities() and Get() are the only round-trips wired in
// v0.x because they are the only ones any current resource needs.
// Subscribe() and Set() are deliberately deferred until a resource
// requires them.
//
// Standard reference: gNMI Specification 0.10.0 (see
// https://github.com/openconfig/reference/blob/master/rpc/gnmi/gnmi-specification.md).
package gnmi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	gnmipb "github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// DefaultPort is the EOS gNMI default port (Arista convention).
const DefaultPort = 6030

// Sentinel errors returned by package gnmi.
var (
	ErrHostRequired    = errors.New("gnmi: host is required")
	ErrEmptyPathString = errors.New("gnmi: path string is empty")
	ErrClientNotInit   = errors.New("gnmi: client not initialised")
	ErrInvalidCABundle = errors.New("gnmi: parse CA bundle: invalid PEM")
)

// Config is the per-device connection configuration.
type Config struct {
	// Host is the management address of the EOS switch (DNS or IP).
	Host string
	// Port is the gNMI gRPC port. Defaults to DefaultPort.
	Port int
	// Username for basic-auth metadata. Optional.
	Username string
	// Password for basic-auth metadata. Optional.
	Password string
	// CACert is a PEM-encoded server CA bundle. When empty, system roots
	// are used (subject to InsecureSkipVerify).
	CACert []byte
	// InsecureSkipVerify disables TLS certificate verification. Use only
	// for development against self-signed lab certs.
	InsecureSkipVerify bool
	// PlaintextNoTLS disables TLS entirely. Some bench setups expose gNMI
	// over plain gRPC. Production deployments must keep this false.
	PlaintextNoTLS bool
}

func (c Config) validate() error {
	if c.Host == "" {
		return ErrHostRequired
	}
	return nil
}

// Client is a long-lived handle around a gRPC connection plus the
// generated gNMIClient stub. Callers must Close the client when done.
type Client struct {
	cfg  Config
	conn *grpc.ClientConn
	stub gnmipb.GNMIClient
}

// Dial constructs the gRPC client for the gNMI target described by cfg.
// The connection is lazy — the underlying gRPC client establishes the
// HTTP/2 link on the first RPC. Use Capabilities() to validate the link.
func Dial(_ context.Context, cfg Config) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	port := cfg.Port
	if port == 0 {
		port = DefaultPort
	}
	addr := fmt.Sprintf("%s:%d", cfg.Host, port)

	creds, err := transportCreds(cfg)
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("gnmi: client %s: %w", addr, err)
	}
	return &Client{cfg: cfg, conn: conn, stub: gnmipb.NewGNMIClient(conn)}, nil
}

// transportCreds derives the gRPC TransportCredentials from cfg.
func transportCreds(cfg Config) (credentials.TransportCredentials, error) {
	if cfg.PlaintextNoTLS {
		return insecure.NewCredentials(), nil
	}
	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cfg.InsecureSkipVerify, //nolint:gosec // explicit dev override; production disables.
	}
	if len(cfg.CACert) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(cfg.CACert) {
			return nil, ErrInvalidCABundle
		}
		tlsCfg.RootCAs = pool
	}
	return credentials.NewTLS(tlsCfg), nil
}

// Close releases the underlying gRPC connection. Idempotent.
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	c.stub = nil
	return err
}

// Capabilities issues a CapabilityRequest. It is the cheapest way to
// prove the gRPC link, TLS handshake, and AAA path are correct.
func (c *Client) Capabilities(ctx context.Context) (*gnmipb.CapabilityResponse, error) {
	if c == nil || c.stub == nil {
		return nil, ErrClientNotInit
	}
	return c.stub.Capabilities(c.withAuth(ctx), &gnmipb.CapabilityRequest{})
}

// Get issues a GetRequest for one or more paths. Encoding is JSON_IETF
// (the lingua franca for OpenConfig models on EOS).
func (c *Client) Get(ctx context.Context, paths []string) (*gnmipb.GetResponse, error) {
	if c == nil || c.stub == nil {
		return nil, ErrClientNotInit
	}
	if len(paths) == 0 {
		return nil, ErrEmptyPathString
	}
	parsed := make([]*gnmipb.Path, 0, len(paths))
	for _, p := range paths {
		gp, err := ParsePath(p)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, gp)
	}
	req := &gnmipb.GetRequest{
		Path:     parsed,
		Encoding: gnmipb.Encoding_JSON_IETF,
	}
	return c.stub.Get(c.withAuth(ctx), req)
}

// withAuth attaches gNMI basic-auth metadata when credentials are set.
// EOS's gNMI implementation honours the `username` / `password` keys
// alongside the canonical `authorization: Basic …` header; we send both
// to maximise interop with mainstream gNMI servers.
func (c *Client) withAuth(ctx context.Context) context.Context {
	if c.cfg.Username == "" && c.cfg.Password == "" {
		return ctx
	}
	pairs := []string{
		"username", c.cfg.Username,
		"password", c.cfg.Password,
	}
	if c.cfg.Username != "" {
		token := base64.StdEncoding.EncodeToString([]byte(c.cfg.Username + ":" + c.cfg.Password))
		pairs = append(pairs, "authorization", "Basic "+token)
	}
	return metadata.AppendToOutgoingContext(ctx, pairs...)
}

// ParsePath parses a slash-delimited gNMI path string into the gNMI
// proto Path. Supports the standard `name[key=value]` element syntax
// (for example `interfaces/interface[name=Ethernet1]/state/counters`).
//
// The input must be non-empty. A leading `/` is tolerated.
func ParsePath(s string) (*gnmipb.Path, error) {
	if s == "" {
		return nil, ErrEmptyPathString
	}
	s = strings.TrimPrefix(s, "/")
	if s == "" {
		return &gnmipb.Path{}, nil
	}
	elems := splitPath(s)
	out := &gnmipb.Path{Elem: make([]*gnmipb.PathElem, 0, len(elems))}
	for _, raw := range elems {
		name, keys := splitElem(raw)
		out.Elem = append(out.Elem, &gnmipb.PathElem{Name: name, Key: keys})
	}
	return out, nil
}

// splitPath splits a gNMI path on `/` while respecting `[...]` key
// brackets so a `/` inside a key value is preserved verbatim.
func splitPath(s string) []string {
	out := make([]string, 0, 4)
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case '/':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// splitElem extracts the element name and any `[k=v]` keys from a path
// element. Multiple keys are allowed (`name[k1=v1][k2=v2]`).
func splitElem(elem string) (string, map[string]string) {
	idx := strings.IndexByte(elem, '[')
	if idx < 0 {
		return elem, nil
	}
	name := elem[:idx]
	rest := elem[idx:]
	keys := make(map[string]string)
	for len(rest) > 0 {
		if rest[0] != '[' {
			break
		}
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			break
		}
		kv := rest[1:end]
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			break
		}
		keys[k] = v
		rest = rest[end+1:]
	}
	return name, keys
}
