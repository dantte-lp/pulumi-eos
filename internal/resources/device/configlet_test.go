package device

import (
	"errors"
	"strings"
	"testing"
)

func TestCanonicalConfigletLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "single_line",
			body: "vlan 100",
			want: []string{"vlan 100"},
		},
		{
			name: "multi_line",
			body: "vlan 100\n   name web\nexit",
			want: []string{"vlan 100", "   name web", "exit"},
		},
		{
			name: "trim_trailing_whitespace",
			body: "vlan 100   \n   name web\t\r\n",
			want: []string{"vlan 100", "   name web"},
		},
		{
			name: "skip_blank_lines",
			body: "vlan 100\n\n   \n\tname web\n",
			want: []string{"vlan 100", "\tname web"},
		},
		{
			name: "all_blank_returns_empty",
			body: "\n\n   \n\t\n",
			want: []string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := canonicalConfigletLines(tc.body)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %d (%v), want %d (%v)", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("line[%d]: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestPrepareConfiglet_Validation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args ConfigletArgs
		err  error
	}{
		{
			name: "missing_name",
			args: ConfigletArgs{Name: "", Body: "vlan 100"},
			err:  ErrConfigletNameRequired,
		},
		{
			name: "whitespace_name",
			args: ConfigletArgs{Name: "   \t", Body: "vlan 100"},
			err:  ErrConfigletNameRequired,
		},
		{
			name: "empty_body",
			args: ConfigletArgs{Name: "ok", Body: ""},
			err:  ErrConfigletContentRequired,
		},
		{
			name: "blank_body",
			args: ConfigletArgs{Name: "ok", Body: "\n\n   \n"},
			err:  ErrConfigletContentRequired,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := prepareConfiglet(tc.args)
			if !errors.Is(err, tc.err) {
				t.Fatalf("got %v, want sentinel %v", err, tc.err)
			}
		})
	}
}

func TestPrepareConfiglet_DigestStable(t *testing.T) {
	t.Parallel()
	a := ConfigletArgs{Name: "x", Body: "vlan 100\n   name web\n"}
	b := ConfigletArgs{Name: "x", Body: "vlan 100   \n\n   name web\t"}

	cmdsA, digestA, err := prepareConfiglet(a)
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	cmdsB, digestB, err := prepareConfiglet(b)
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	if digestA != digestB {
		t.Fatalf("digest must be stable across whitespace differences: %s vs %s", digestA, digestB)
	}
	if len(cmdsA) != len(cmdsB) {
		t.Fatalf("canonical command count differs: %v vs %v", cmdsA, cmdsB)
	}
	if !strings.HasPrefix(digestA, "") || len(digestA) != 64 {
		t.Fatalf("digest must be 64-char hex sha256: %q", digestA)
	}
}

func TestPrepareConfiglet_DigestDiffersOnContent(t *testing.T) {
	t.Parallel()
	_, d1, err := prepareConfiglet(ConfigletArgs{Name: "x", Body: "vlan 100"})
	if err != nil {
		t.Fatalf("d1: %v", err)
	}
	_, d2, err := prepareConfiglet(ConfigletArgs{Name: "x", Body: "vlan 200"})
	if err != nil {
		t.Fatalf("d2: %v", err)
	}
	if d1 == d2 {
		t.Fatalf("digests must differ for different bodies: %s == %s", d1, d2)
	}
}

func TestConfigletID(t *testing.T) {
	t.Parallel()
	if got := configletID("border-uplinks"); got != "configlet/border-uplinks" {
		t.Fatalf("got %q, want %q", got, "configlet/border-uplinks")
	}
}

func TestSanitizeForSession(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"plain", "plain"},
		{"with/slash", "with-slash"},
		{"with space", "with-space"},
		{"with:colon", "with-colon"},
		{"with.dot", "with-dot"},
		{"a/b c:d.e", "a-b-c-d-e"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeForSession(tc.in); got != tc.want {
				t.Fatalf("sanitizeForSession(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
