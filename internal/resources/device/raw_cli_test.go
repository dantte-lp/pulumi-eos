package device

import (
	"errors"
	"testing"
)

func TestPrepareRawCli_Validation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args RawCliArgs
		err  error
	}{
		{
			name: "missing_name",
			args: RawCliArgs{Name: "", Body: "vlan 100"},
			err:  ErrRawCliNameRequired,
		},
		{
			name: "whitespace_name",
			args: RawCliArgs{Name: "  \t", Body: "vlan 100"},
			err:  ErrRawCliNameRequired,
		},
		{
			name: "empty_body",
			args: RawCliArgs{Name: "ok", Body: ""},
			err:  ErrRawCliContentRequired,
		},
		{
			name: "blank_body",
			args: RawCliArgs{Name: "ok", Body: "   \n\n\t"},
			err:  ErrRawCliContentRequired,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := prepareRawCli(tc.args)
			if !errors.Is(err, tc.err) {
				t.Fatalf("got %v, want %v", err, tc.err)
			}
		})
	}
}

func TestPrepareRawCli_DigestStable(t *testing.T) {
	t.Parallel()
	a := RawCliArgs{Name: "x", Body: "vlan 100\n   name web\n"}
	b := RawCliArgs{Name: "x", Body: "vlan 100   \n\n   name web\t"}
	_, da, err := prepareRawCli(a)
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	_, db, err := prepareRawCli(b)
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	if da != db {
		t.Fatalf("digest must be stable across whitespace differences: %s vs %s", da, db)
	}
	if len(da) != 64 {
		t.Fatalf("digest must be 64-char hex sha256: %q", da)
	}
}

func TestRawCliID(t *testing.T) {
	t.Parallel()
	if got := rawCliID("uplink-acl"); got != "rawCli/uplink-acl" {
		t.Fatalf("got %q, want %q", got, "rawCli/uplink-acl")
	}
}

func TestIsEmptyDiff(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		diff string
		want bool
	}{
		{"empty_string", "", true},
		{"only_whitespace", "  \n\t\n", true},
		{"header_only", "--- system:/running-config\n+++ session:/the-name", true},
		{
			name: "header_with_blank_line_padding",
			diff: "--- system:/running-config\n\n+++ session:/the-name\n\n",
			want: true,
		},
		{
			name: "real_diff",
			diff: "--- a\n+++ b\n@@ -1 +1,2 @@\n+vlan 100",
			want: false,
		},
		{
			name: "diff_without_headers",
			diff: "+vlan 100",
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isEmptyDiff(tc.diff); got != tc.want {
				t.Fatalf("isEmptyDiff(%q) = %v, want %v", tc.diff, got, tc.want)
			}
		})
	}
}
