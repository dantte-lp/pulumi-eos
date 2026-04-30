package l3

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateRcf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args RcfArgs
		want error
	}{
		{
			name: "ok_named",
			args: RcfArgs{Name: "EVPN_RCF", SourceFile: "flash:rcf-evpn.txt"},
		},
		{
			name: "ok_default",
			args: RcfArgs{Name: "default", SourceFile: "flash:default.txt"},
		},
		{"empty_name", RcfArgs{SourceFile: "flash:x.txt"}, ErrRcfNameRequired},
		{"name_with_space", RcfArgs{Name: "BAD NAME", SourceFile: "flash:x.txt"}, ErrRcfBadName},
		{"name_starts_with_digit", RcfArgs{Name: "1RCF", SourceFile: "flash:x.txt"}, ErrRcfBadName},
		{"name_dot", RcfArgs{Name: "my.rcf", SourceFile: "flash:x.txt"}, ErrRcfBadName},
		{"empty_source", RcfArgs{Name: "RCF"}, ErrRcfSourceFileEmpty},
		{"source_no_storage", RcfArgs{Name: "RCF", SourceFile: "rcf.txt"}, ErrRcfBadSourcePath},
		{"source_with_space", RcfArgs{Name: "RCF", SourceFile: "flash:my file.txt"}, ErrRcfBadSourcePath},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateRcf(tc.args)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("unexpected: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestBuildRcfCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		args   RcfArgs
		remove bool
		want   []string
	}{
		{
			name: "named_create",
			args: RcfArgs{Name: "EVPN_RCF", SourceFile: "flash:rcf-evpn.txt"},
			want: []string{
				"router general",
				"control-functions",
				"code unit EVPN_RCF source pulled-from flash:rcf-evpn.txt",
				"exit",
				"exit",
			},
		},
		{
			name: "default_create",
			args: RcfArgs{Name: "default", SourceFile: "flash:default.txt"},
			want: []string{
				"router general",
				"control-functions",
				"code source pulled-from flash:default.txt",
				"exit",
				"exit",
			},
		},
		{
			name:   "named_delete",
			args:   RcfArgs{Name: "EVPN_RCF"},
			remove: true,
			want: []string{
				"router general",
				"control-functions",
				"no code unit EVPN_RCF source pulled-from",
				"exit",
				"exit",
			},
		},
		{
			name:   "default_delete",
			args:   RcfArgs{Name: "default"},
			remove: true,
			want: []string{
				"router general",
				"control-functions",
				"no code source pulled-from",
				"exit",
				"exit",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildRcfCmds(tc.args, tc.remove)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch:\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("cmd[%d]: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestRcfID(t *testing.T) {
	t.Parallel()
	if got := rcfID("EVPN_RCF"); got != "rcf/EVPN_RCF" {
		t.Fatalf("got %q", got)
	}
}

func TestRcfTargetClause(t *testing.T) {
	t.Parallel()
	if got := rcfTargetClause("default"); got != "code" {
		t.Fatalf("default → %q", got)
	}
	if got := rcfTargetClause("FOO"); got != "code unit FOO" {
		t.Fatalf("named → %q", got)
	}
}

func TestParseRcfSection(t *testing.T) {
	t.Parallel()
	out := strings.Join([]string{
		"router general",
		"   control-functions",
		"      code source pulled-from flash:default.txt",
		"      code unit EVPN_RCF source pulled-from flash:rcf-evpn.txt",
		"      code unit OTHER source pulled-from flash:other.txt edited",
	}, "\n")

	defaultRow, ok := parseRcfSection(out, "default")
	if !ok {
		t.Fatal("default unit not found")
	}
	if defaultRow.SourceFile != "flash:default.txt" {
		t.Fatalf("default sourceFile = %q", defaultRow.SourceFile)
	}

	evpnRow, ok := parseRcfSection(out, "EVPN_RCF")
	if !ok {
		t.Fatal("EVPN_RCF unit not found")
	}
	if evpnRow.SourceFile != "flash:rcf-evpn.txt" {
		t.Fatalf("EVPN_RCF sourceFile = %q", evpnRow.SourceFile)
	}

	editedRow, ok := parseRcfSection(out, "OTHER")
	if !ok {
		t.Fatal("OTHER unit not found")
	}
	if editedRow.SourceFile != "flash:other.txt" {
		t.Fatalf("OTHER edited-suffix not stripped: %q", editedRow.SourceFile)
	}

	if _, ok := parseRcfSection(out, "MISSING"); ok {
		t.Fatal("must not match unknown name")
	}
}
