//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestEAPI_Device_ShowVersionFacts verifies the same `show version`
// JSON path the eos:device:Device resource consumes. The resource maps
// modelName / serialNumber / version / systemMacAddress / hardwareRevision
// into DeviceState; here we assert the eAPI surface continues to
// expose those fields against cEOS 4.36 — a regression here would
// silently zero device facts during refresh / drift detection.
func TestEAPI_Device_ShowVersionFacts(t *testing.T) {
	t.Parallel()
	cli := newTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := cli.RunCmds(ctx, []string{"show version"}, "json")
	if err != nil {
		t.Fatalf("show version: %v", err)
	}
	if len(resp) == 0 {
		t.Fatal("show version returned no rows")
	}
	row := resp[0]

	// Required-by-resource fields.
	required := []string{
		"modelName",
		"serialNumber",
		"version",
		"systemMacAddress",
	}
	for _, k := range required {
		v, ok := row[k].(string)
		if !ok {
			t.Errorf("show version: field %q missing or non-string (got %T)", k, row[k])
			continue
		}
		if strings.TrimSpace(v) == "" {
			t.Errorf("show version: field %q is empty", k)
		}
	}

	// cEOS reports model `cEOSLab` — sanity-check that a meaningful
	// value lands in `modelName` rather than e.g. an empty string or a
	// JSON number we'd silently coerce to "".
	if model, _ := row["modelName"].(string); !strings.Contains(model, "EOS") {
		t.Errorf("modelName=%q does not contain 'EOS'", model)
	}
}
