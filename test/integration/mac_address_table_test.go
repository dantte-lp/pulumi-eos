//go:build integration

package integration

import (
	"context"
	"testing"
	"time"
)

// TestEAPI_MacAddressTable_Shape calls `show mac address-table` against
// live cEOS to prove the eAPI command + JSON shape are stable. cEOS Lab
// runs without external interfaces, so the unicastTable is expected to be
// empty — the test asserts the command succeeds and the response carries
// the structural keys the eos:l2:macAddressTable function reads.
func TestEAPI_MacAddressTable_Shape(t *testing.T) {
	t.Parallel()
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := cli.RunCmds(ctx, []string{"show mac address-table"}, "json")
	if err != nil {
		t.Fatalf("show mac address-table: %v", err)
	}
	if len(resp) == 0 {
		t.Fatal("show mac address-table returned no result")
	}
	uni, ok := resp[0]["unicastTable"].(map[string]any)
	if !ok {
		t.Fatalf("missing unicastTable in response: %+v", resp[0])
	}
	if _, ok := uni["tableEntries"]; !ok {
		t.Fatalf("unicastTable missing tableEntries key: %+v", uni)
	}
}
