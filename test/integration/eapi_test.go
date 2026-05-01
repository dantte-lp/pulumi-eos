//go:build integration

// Package integration runs end-to-end checks against a live Arista cEOS Lab
// container brought up via deployments/compose/compose.integration.yml.
//
// It is gated by the `integration` build tag so unit-test runs (`go test
// ./...`) skip it. To run:
//
//	make test-integration-up      # brings up cEOS + applies bootstrap config
//	make test-integration         # invokes this package
//	make test-integration-down    # tears the stack down
package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dantte-lp/pulumi-eos/internal/client/eapi"
)

// envOr returns the environment variable's value or fallback.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// newTestClient builds an eAPI client pointed at the local cEOS service.
//
// Defaults match deployments/compose/compose.integration.yml port mapping
// (127.0.0.1:18080 → cEOS :80) and the bootstrap-config admin user.
func newTestClient(t *testing.T) *eapi.Client {
	t.Helper()
	host := envOr("EOS_HOST", "127.0.0.1")
	port := 18080
	cfg := eapi.Config{
		Host:     host,
		Port:     port,
		Username: envOr("EOS_USERNAME", "admin"),
		Password: envOr("EOS_PASSWORD", "admin"),
		Timeout:  10 * time.Second,
		UseHTTPS: false,
	}
	cli, err := eapi.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("eapi.New: %v", err)
	}
	return cli
}

// TestEAPI_ShowVersion is the canary integration test. It proves the
// dev-container test runner can reach the cEOS instance over eAPI and that
// the goeapi-backed client can decode a structured response.
func TestEAPI_ShowVersion(t *testing.T) {
	t.Parallel()
	cli := newTestClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, err := cli.RunCmds(ctx, []string{"show version"}, "json")
	if err != nil {
		t.Fatalf("show version: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("show version returned no result")
	}
	ver, ok := out[0]["version"].(string)
	if !ok || ver == "" {
		t.Fatalf("missing version field; raw: %+v", out[0])
	}
	t.Logf("cEOS reports version=%s", ver)
}

// TestEAPI_ConfigSession_AbortIsClean exercises the Session lifecycle without
// committing: open → stage a benign command → abort. It proves the
// 1-slot semaphore release path is correct so subsequent tests can open
// their own session.
func TestEAPI_ConfigSession_AbortIsClean(t *testing.T) {
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const sessName = "pulumi-eos-it-canary"
	sess, err := cli.OpenSession(ctx, sessName)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	if stageErr := sess.Stage(ctx, []string{"vlan 999", "name pulumi-it-canary"}); stageErr != nil {
		_ = sess.Abort(ctx)
		t.Fatalf("Stage: %v", stageErr)
	}

	if abortErr := sess.Abort(ctx); abortErr != nil {
		t.Fatalf("Abort: %v", abortErr)
	}

	// A second OpenSession must succeed if Abort released the slot.
	sess2, err := cli.OpenSession(ctx, sessName+"-2")
	if err != nil {
		t.Fatalf("OpenSession (after abort): %v", err)
	}
	if abortErr := sess2.Abort(ctx); abortErr != nil {
		t.Fatalf("Abort second: %v", abortErr)
	}
}
