//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/dantte-lp/pulumi-eos/internal/client/gnmi"
)

// TestGNMI_Capabilities exercises the minimum gNMI client against the
// live cEOS gNMI listener bootstrapped by scripts/integration-bootstrap.sh
// (transport grpc default, port 6030 → host 18830). The test validates:
//
//  1. Dial against the lazy gRPC client returns no error.
//  2. Capabilities() returns a non-empty SupportedEncodings slice (the
//     EOS gNMI server always advertises at least JSON_IETF and PROTO).
//
// The test is skipped automatically when gNMI is not reachable so a
// partially-bootstrapped cEOS does not block the integration suite.
func TestGNMI_Capabilities(t *testing.T) {
	host := envOr("GNMI_HOST", "127.0.0.1")
	port := 18830
	cli, err := gnmi.Dial(context.Background(), gnmi.Config{
		Host:           host,
		Port:           port,
		Username:       envOr("EOS_USERNAME", "admin"),
		Password:       envOr("EOS_PASSWORD", "admin"),
		PlaintextNoTLS: true,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() {
		_ = cli.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := cli.Capabilities(ctx)
	if err != nil {
		if os.Getenv("GNMI_REQUIRED") == "" {
			t.Skipf("gNMI Capabilities unreachable (skip via GNMI_REQUIRED=1 to fail loudly): %v", err)
		}
		t.Fatalf("Capabilities: %v", err)
	}
	if resp.GetGNMIVersion() == "" {
		t.Fatalf("empty gNMIVersion in CapabilityResponse: %+v", resp)
	}
	if len(resp.GetSupportedEncodings()) == 0 {
		t.Fatalf("empty SupportedEncodings in CapabilityResponse: %+v", resp)
	}
	t.Logf("gNMI %s, encodings=%v, models=%d",
		resp.GetGNMIVersion(),
		resp.GetSupportedEncodings(),
		len(resp.GetSupportedModels()),
	)
}
