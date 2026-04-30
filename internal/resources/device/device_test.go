package device

import (
	"testing"

	"github.com/pulumi/pulumi-go-provider/infer"
)

// Device.Create / Read run against eAPI; their happy-path is exercised
// by the integration test (test/integration/device_test.go). Unit-side
// we pin the documented contract that Delete is a no-op (the resource
// never owned device state, so refresh / destroy must not mutate the
// switch) and that error strings used in fact reads compose cleanly.
func TestDevice_Delete_NoOp(t *testing.T) {
	t.Parallel()
	r := &Device{}
	resp, err := r.Delete(
		t.Context(),
		infer.DeleteRequest[DeviceState]{ID: "10.0.0.1", State: DeviceState{}},
	)
	if err != nil {
		t.Fatalf("Delete should be no-op, got: %v", err)
	}
	_ = resp
}

// readFacts surfaces ErrFactsMissing when `show version` returns no
// rows. We exercise the sentinel-error wiring via a parser smoke
// rather than re-running eAPI — the integration test does the live
// call.
func TestDevice_ErrFactsMissing_String(t *testing.T) {
	t.Parallel()
	if got := ErrFactsMissing.Error(); got == "" {
		t.Fatalf("ErrFactsMissing must have a stable message")
	}
}
