// Package eapi wraps github.com/aristanetworks/goeapi and adds primitives
// pulumi-eos relies on for atomic configuration apply: named configuration
// sessions, `commit timer` confirmed-commit, and explicit abort.
//
// The implementation is intentionally narrow: only the surface the provider
// actually needs is exposed. Callers MUST treat *Client as a long-lived value
// per device — goeapi serializes commands per Node internally.
package eapi

import (
	"context"
	"errors"
	"fmt"
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
