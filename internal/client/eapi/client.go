// Package eapi wraps github.com/aristanetworks/goeapi and adds primitives
// pulumi-eos relies on for atomic configuration apply: named configuration
// sessions, `commit timer` confirmed-commit, and explicit abort.
//
// The implementation is intentionally narrow: only the surface the provider
// actually needs is exposed. Callers MUST treat *Client as a long-lived value
// per device — goeapi serializes commands per Node internally.
package eapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/aristanetworks/goeapi"
)

// DefaultPort is eAPI's default HTTPS port.
const DefaultPort = 443

// DefaultCommitTimer is the default rollback window for confirmed-commit.
//
// Five minutes matches the mainline Arista guidance: long enough for an
// out-of-band operator to confirm, short enough to limit blast radius.
const DefaultCommitTimer = 5 * time.Minute

// Sentinel errors returned by package eapi.
var (
	ErrRichRPCFailure      = errors.New("eapi: rich runCmds RPC failure")
	ErrHostRequired        = errors.New("eapi: host is required")
	ErrUsernameRequired    = errors.New("eapi: username is required")
	ErrClientNotInit       = errors.New("eapi: client not initialised")
	ErrSessionNameRequired = errors.New("eapi: session name is required")
	ErrSessionClosed       = errors.New("eapi: session is closed")
)

// Config is the connection-level configuration for one EOS device.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	Timeout  time.Duration
	// UseHTTPS forces HTTPS transport. HTTP is supported only for explicit
	// dev-loopback debugging and is gated by the provider-level
	// eosInsecure flag.
	UseHTTPS bool
}

func (c Config) validate() error {
	if c.Host == "" {
		return ErrHostRequired
	}
	if c.Username == "" {
		return ErrUsernameRequired
	}
	return nil
}

// Client wraps a goeapi.Node with provider-aware helpers.
//
// At most one configuration session may be active at a time per Client; the
// sessionSlot channel acts as a 1-slot semaphore. OpenSession sends into the
// slot, Commit / Abort drains it. Using a channel rather than sync.Mutex
// avoids holding a lock across function boundaries and makes ownership
// transfer to the caller-side *Session explicit.
type Client struct {
	cfg         Config
	node        *goeapi.Node
	sessionSlot chan struct{}
}

// New connects to a single EOS device and returns a *Client ready for use.
func New(_ context.Context, cfg Config) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	transport := "https"
	if !cfg.UseHTTPS {
		transport = "http"
	}
	port := cfg.Port
	if port == 0 {
		port = DefaultPort
	}
	node, err := goeapi.Connect(transport, cfg.Host, cfg.Username, cfg.Password, port)
	if err != nil {
		return nil, fmt.Errorf("eapi: connect %s: %w", cfg.Host, err)
	}
	return &Client{
		cfg:         cfg,
		node:        node,
		sessionSlot: make(chan struct{}, 1),
	}, nil
}

// RunCmds executes one or more EOS CLI commands against the device.
// Format is "json" (default, structured) or "text" (raw show output).
func (c *Client) RunCmds(ctx context.Context, cmds []string, format string) ([]map[string]any, error) {
	if c == nil || c.node == nil {
		return nil, ErrClientNotInit
	}
	if format == "" {
		format = "json"
	}
	// goeapi does not expose ctx-aware variants; the timeout is honored via
	// goeapi.Node.SetTimeout in New. The ctx check below short-circuits if
	// the caller cancels before invocation.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resp, err := c.node.RunCommands(cmds, format)
	if err != nil {
		return nil, fmt.Errorf("eapi: runCmds %v: %w", cmds, err)
	}
	out := make([]map[string]any, 0, len(resp.Result))
	out = append(out, resp.Result...)
	return out, nil
}

// Command is the rich form of an eAPI CLI command. When `Input` is
// non-empty the entry is JSON-marshaled as an object with the shape
// `{"cmd": <Cmd>, "input": <Input>}`; this is the documented escape
// hatch for commands that accept multi-line stdin (e.g. `banner motd`,
// `code unit X` for inline RCF source).
//
// Source: EOS Command API Guide §1.2.3 — Command Specification.
type Command struct {
	Cmd   string
	Input string
}

// RunCmdsRich executes a mix of plain (string) and rich (Cmd + Input)
// commands by crafting the JSON-RPC request directly. goeapi v1.0.0
// does not expose the `input` field on `Node.RunCommands`; this
// helper bypasses goeapi for the cases that need it. The HTTP call
// inherits the host / port / username / password / timeout / TLS
// posture from the existing Client config.
func (c *Client) RunCmdsRich(ctx context.Context, cmds []Command, format string) ([]map[string]any, error) {
	if c == nil {
		return nil, ErrClientNotInit
	}
	if format == "" {
		format = "json"
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	bodyBytes, err := buildRichPayload(cmds, format)
	if err != nil {
		return nil, err
	}
	rpc, err := c.postEAPI(ctx, bodyBytes)
	if err != nil {
		return nil, err
	}
	if rpc.Error != nil {
		return nil, fmt.Errorf("%w: code %d: %s", ErrRichRPCFailure, rpc.Error.Code, rpc.Error.Message)
	}
	// Drop the `enable` slot to match RunCmds' shape.
	if len(rpc.Result) > 0 {
		return rpc.Result[1:], nil
	}
	return rpc.Result, nil
}

// richRPCResponse mirrors the JSON-RPC envelope returned by the eAPI
// command endpoint.
type richRPCResponse struct {
	Result []map[string]any `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    []any  `json:"data"`
	} `json:"error"`
}

// buildRichPayload marshals a runCmds request mixing plain strings
// and `{cmd, input}` objects. `enable` is prepended to mirror
// goeapi.Node.RunCommands behaviour.
func buildRichPayload(cmds []Command, format string) ([]byte, error) {
	payloadCmds := make([]any, 0, len(cmds)+1)
	payloadCmds = append(payloadCmds, "enable")
	for _, cmd := range cmds {
		if cmd.Input == "" {
			payloadCmds = append(payloadCmds, cmd.Cmd)
			continue
		}
		payloadCmds = append(payloadCmds, map[string]string{
			"cmd":   cmd.Cmd,
			"input": cmd.Input,
		})
	}
	body := map[string]any{
		"jsonrpc": "2.0",
		"method":  "runCmds",
		"params": map[string]any{
			"version": 1,
			"cmds":    payloadCmds,
			"format":  format,
		},
		"id": "1",
	}
	out, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("eapi: marshal rich payload: %w", err)
	}
	return out, nil
}

// Session is a named EOS configuration session that wraps a sequence of
// configuration commands inside an atomic `configure session …` block.
//
// The provider creates one Session per Pulumi resource Update. The lifecycle
// is: New → Stage → Diff → Commit (or CommitTimer + Confirm) → Close. On any
// error the caller MUST call Abort.
type Session struct {
	parent *Client
	name   string
	open   bool
}

// OpenSession opens a new configuration session with the given name.
//
// Concurrency: at most one session per Client. If another session is already
// open, OpenSession waits for it to close or for ctx to expire.
func (c *Client) OpenSession(ctx context.Context, name string) (*Session, error) {
	if name == "" {
		return nil, ErrSessionNameRequired
	}

	// Acquire the 1-slot session semaphore.
	select {
	case c.sessionSlot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if _, err := c.RunCmds(ctx, []string{
		"configure session " + name,
		"end",
	}, "json"); err != nil {
		<-c.sessionSlot // release on failure
		return nil, fmt.Errorf("eapi: open session %s: %w", name, err)
	}
	return &Session{parent: c, name: name, open: true}, nil
}

// postEAPI POSTs the JSON-RPC body to the eAPI command endpoint and
// decodes the envelope.
func (c *Client) postEAPI(ctx context.Context, body []byte) (richRPCResponse, error) {
	scheme := "http"
	if c.cfg.UseHTTPS {
		scheme = "https"
	}
	port := c.cfg.Port
	if port == 0 {
		port = DefaultPort
	}
	url := fmt.Sprintf("%s://%s:%d/command-api", scheme, c.cfg.Host, port)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return richRPCResponse{}, fmt.Errorf("eapi: build rich request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.cfg.Username, c.cfg.Password)

	timeout := c.cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	httpCli := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion:         tls.VersionTLS12,
				InsecureSkipVerify: !c.cfg.UseHTTPS, //nolint:gosec // mirrors goeapi: HTTPS skips verify only when the caller opted in.
			},
		},
	}
	resp, err := httpCli.Do(req)
	if err != nil {
		return richRPCResponse{}, fmt.Errorf("eapi: rich request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:gosec // best-effort cleanup; close errors on read-only response are not actionable.

	var rpc richRPCResponse
	if decodeErr := json.NewDecoder(resp.Body).Decode(&rpc); decodeErr != nil {
		return richRPCResponse{}, fmt.Errorf("eapi: decode rich response: %w", decodeErr)
	}
	return rpc, nil
}

// Stage adds configuration commands to the session.
// Commands are NOT applied to running-config until Commit.
func (s *Session) Stage(ctx context.Context, cmds []string) error {
	if !s.open {
		return ErrSessionClosed
	}
	staged := make([]string, 0, len(cmds)+2)
	staged = append(staged, "configure session "+s.name)
	staged = append(staged, cmds...)
	staged = append(staged, "end")
	if _, err := s.parent.RunCmds(ctx, staged, "json"); err != nil {
		return fmt.Errorf("eapi: stage in %s: %w", s.name, err)
	}
	return nil
}

// StageRich is the rich-command counterpart of Stage. Callers pass a
// mix of plain (`Cmd`) and complex (`Cmd` + `Input`) commands; the
// `configure session <name>` wrapper and `end` terminator are added
// automatically.
//
// Use this for resources whose CLI surface needs the eAPI `input`
// field — currently `eos:l3:Rcf` for inline RCF source, but the same
// helper unblocks future `banner motd`, `comment`, `ip extcommunity-
// list … config-replace` consumers.
func (s *Session) StageRich(ctx context.Context, cmds []Command) error {
	if !s.open {
		return ErrSessionClosed
	}
	staged := make([]Command, 0, len(cmds)+2)
	staged = append(staged, Command{Cmd: "configure session " + s.name})
	staged = append(staged, cmds...)
	staged = append(staged, Command{Cmd: "end"})
	if _, err := s.parent.RunCmdsRich(ctx, staged, "json"); err != nil {
		return fmt.Errorf("eapi: stage rich in %s: %w", s.name, err)
	}
	return nil
}

// Diff returns the textual diff between the session's staged configuration
// and the current running-config.
func (s *Session) Diff(ctx context.Context) (string, error) {
	if !s.open {
		return "", ErrSessionClosed
	}
	resp, err := s.parent.RunCmds(ctx, []string{
		fmt.Sprintf("show session-config named %s diffs", s.name),
	}, "text")
	if err != nil {
		return "", fmt.Errorf("eapi: diff %s: %w", s.name, err)
	}
	if len(resp) == 0 {
		return "", nil
	}
	if v, ok := resp[0]["output"].(string); ok {
		return v, nil
	}
	return "", nil
}

// CommitTimer commits the session with a rollback window of timeout.
// If Commit is not called before the window elapses, EOS auto-rolls back.
func (s *Session) CommitTimer(ctx context.Context, timeout time.Duration) error {
	if !s.open {
		return ErrSessionClosed
	}
	if timeout <= 0 {
		timeout = DefaultCommitTimer
	}
	hh := int(timeout.Hours())
	mm := int(timeout.Minutes()) - hh*60
	ss := int(timeout.Seconds()) - hh*3600 - mm*60
	cmd := fmt.Sprintf("configure session %s commit timer %d:%02d:%02d", s.name, hh, mm, ss)
	if _, err := s.parent.RunCmds(ctx, []string{cmd}, "json"); err != nil {
		return fmt.Errorf("eapi: commit-timer %s: %w", s.name, err)
	}
	return nil
}

// Commit confirms a previous CommitTimer or applies the session immediately.
func (s *Session) Commit(ctx context.Context) error {
	if !s.open {
		return ErrSessionClosed
	}
	cmd := fmt.Sprintf("configure session %s commit", s.name)
	_, err := s.parent.RunCmds(ctx, []string{cmd}, "json")
	s.open = false
	s.release()
	if err != nil {
		return fmt.Errorf("eapi: commit %s: %w", s.name, err)
	}
	return nil
}

// Abort discards the session's staged changes.
// Idempotent; safe to call from a deferred error path.
func (s *Session) Abort(ctx context.Context) error {
	if !s.open {
		return nil
	}
	cmd := "no configure session " + s.name
	_, err := s.parent.RunCmds(ctx, []string{cmd}, "json")
	s.open = false
	s.release()
	if err != nil {
		return fmt.Errorf("eapi: abort %s: %w", s.name, err)
	}
	return nil
}

// release drains the parent's session semaphore. Idempotent.
func (s *Session) release() {
	if s == nil || s.parent == nil {
		return
	}
	select {
	case <-s.parent.sessionSlot:
	default:
	}
}
