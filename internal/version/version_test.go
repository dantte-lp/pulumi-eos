package version

import (
	"strings"
	"testing"
)

func TestFull_FormatStable(t *testing.T) {
	t.Parallel()
	got := Full()
	for _, want := range []string{"version=", "commit=", "built="} {
		if !strings.Contains(got, want) {
			t.Fatalf("Full() = %q, missing %q", got, want)
		}
	}
}

func TestFull_DefaultsAreDev(t *testing.T) {
	t.Parallel()
	if Version != "dev" {
		t.Skipf("Version=%q (set via ldflags); skipping default-value assertion", Version)
	}
	if GitCommit != "unknown" || BuildDate != "unknown" {
		t.Skipf("metadata set via ldflags; skipping default-value assertion")
	}
	got := Full()
	const want = "version=dev commit=unknown built=unknown"
	if got != want {
		t.Fatalf("Full() = %q, want %q", got, want)
	}
}
