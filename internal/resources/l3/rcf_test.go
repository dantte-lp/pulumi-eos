package l3

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateRcf_Name(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args RcfArgs
		want error
	}{
		{
			name: "ok_named_with_code",
			args: RcfArgs{Name: "EVPN_RCF", Code: new("function ACCEPT() { return true; }\n")},
		},
		{
			name: "ok_default_with_code",
			args: RcfArgs{Name: "default", Code: new("function ACCEPT() { return true; }\n")},
		},
		{"empty_name", RcfArgs{Code: new("function X() {}\n")}, ErrRcfNameRequired},
		{"name_with_space", RcfArgs{Name: "BAD NAME", Code: new("function X() {}\n")}, ErrRcfBadName},
		{"name_starts_with_digit", RcfArgs{Name: "1RCF", Code: new("function X() {}\n")}, ErrRcfBadName},
		{"name_dot", RcfArgs{Name: "my.rcf", Code: new("function X() {}\n")}, ErrRcfBadName},
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

func TestValidateRcf_Delivery(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args RcfArgs
		want error
	}{
		{
			name: "code_only_ok",
			args: RcfArgs{Name: "RCF", Code: new("function X() { return true; }\n")},
		},
		{
			name: "source_file_only_ok",
			args: RcfArgs{Name: "RCF", SourceFile: new("flash:rcf.txt")},
		},
		{
			name: "source_url_http_ok",
			args: RcfArgs{Name: "RCF", SourceUrl: new("http://example.net/rcf.txt")},
		},
		{
			name: "source_url_https_ok",
			args: RcfArgs{Name: "RCF", SourceUrl: new("https://example.net/rcf.txt")},
		},
		{
			name: "source_url_ftp_ok",
			args: RcfArgs{Name: "RCF", SourceUrl: new("ftp://example.net/rcf.txt")},
		},
		{"none_set", RcfArgs{Name: "RCF"}, ErrRcfDeliveryRequired},
		{
			name: "code_and_file",
			args: RcfArgs{Name: "RCF", Code: new("function X() {}\n"), SourceFile: new("flash:x.txt")},
			want: ErrRcfDeliveryConflict,
		},
		{
			name: "file_and_url",
			args: RcfArgs{Name: "RCF", SourceFile: new("flash:x.txt"), SourceUrl: new("http://x.net/x.txt")},
			want: ErrRcfDeliveryConflict,
		},
		{
			name: "all_three",
			args: RcfArgs{Name: "RCF", Code: new("f X() {}\n"), SourceFile: new("flash:x.txt"), SourceUrl: new("http://x.net/x.txt")},
			want: ErrRcfDeliveryConflict,
		},
		{
			name: "bad_source_file",
			args: RcfArgs{Name: "RCF", SourceFile: new("rcf.txt")},
			want: ErrRcfBadSourcePath,
		},
		{
			name: "bad_source_file_with_space",
			args: RcfArgs{Name: "RCF", SourceFile: new("flash:my file.txt")},
			want: ErrRcfBadSourcePath,
		},
		{
			name: "bad_source_url_no_scheme",
			args: RcfArgs{Name: "RCF", SourceUrl: new("example.net/rcf.txt")},
			want: ErrRcfBadSourceURL,
		},
		{
			name: "bad_source_url_unknown_scheme",
			args: RcfArgs{Name: "RCF", SourceUrl: new("gemini://example.net/rcf.txt")},
			want: ErrRcfBadSourceURL,
		},
		{
			name: "code_missing_trailing_newline",
			args: RcfArgs{Name: "RCF", Code: new("function X() { return true; }")},
			want: ErrRcfCodeMissingTrailN,
		},
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

func TestBuildRcfPlainCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		args   RcfArgs
		remove bool
		want   []string
	}{
		{
			name: "source_file_named",
			args: RcfArgs{Name: "EVPN_RCF", SourceFile: new("flash:rcf-evpn.txt")},
			want: []string{
				"router general",
				"control-functions",
				"code unit EVPN_RCF source pulled-from flash:rcf-evpn.txt",
				"exit",
				"exit",
			},
		},
		{
			name: "source_file_default",
			args: RcfArgs{Name: "default", SourceFile: new("flash:default.txt")},
			want: []string{
				"router general",
				"control-functions",
				"code source pulled-from flash:default.txt",
				"exit",
				"exit",
			},
		},
		{
			name: "source_url_named",
			args: RcfArgs{Name: "EVPN_RCF", SourceUrl: new("http://example.net/rcf.txt")},
			want: []string{
				"router general",
				"control-functions",
				"pull unit EVPN_RCF replace http://example.net/rcf.txt",
				"exit",
				"exit",
			},
		},
		{
			name: "source_url_default",
			args: RcfArgs{Name: "default", SourceUrl: new("https://example.net/rcf.txt")},
			want: []string{
				"router general",
				"control-functions",
				"pull replace https://example.net/rcf.txt",
				"exit",
				"exit",
			},
		},
		{
			name:   "delete_named",
			args:   RcfArgs{Name: "EVPN_RCF"},
			remove: true,
			want: []string{
				"router general",
				"control-functions",
				"no code unit EVPN_RCF source pulled-from",
				"no code unit EVPN_RCF",
				"exit",
				"exit",
			},
		},
		{
			name:   "delete_default",
			args:   RcfArgs{Name: "default"},
			remove: true,
			want: []string{
				"router general",
				"control-functions",
				"no code source pulled-from",
				"no code",
				"exit",
				"exit",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildRcfPlainCmds(tc.args, tc.remove)
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

func TestBuildRcfRichCmds(t *testing.T) {
	t.Parallel()
	args := RcfArgs{
		Name: "EVPN_RCF",
		Code: new("function ACCEPT_ALL() {\n  return true;\n}\n"),
	}
	got := buildRcfRichCmds(args)
	if len(got) != 5 {
		t.Fatalf("got %d cmds, want 5", len(got))
	}
	if got[0].Cmd != "router general" || got[1].Cmd != "control-functions" {
		t.Fatalf("wrapper cmds wrong: %+v", got[:2])
	}
	if got[2].Cmd != "code unit EVPN_RCF" {
		t.Fatalf("body cmd = %q", got[2].Cmd)
	}
	wantInput := "function ACCEPT_ALL() {\n  return true;\n}\nEOF\n"
	if got[2].Input != wantInput {
		t.Fatalf("input = %q, want %q", got[2].Input, wantInput)
	}
}

func TestBuildRcfRichCmds_AppendsTrailingNewline(t *testing.T) {
	t.Parallel()
	// The validator rejects code without a trailing newline, but the
	// renderer is also defensive — verify it appends one if absent.
	args := RcfArgs{
		Name: "default",
		Code: new("function X() { return true; }"),
	}
	got := buildRcfRichCmds(args)
	if !strings.HasSuffix(got[2].Input, "\nEOF\n") {
		t.Fatalf("input missing \\nEOF\\n suffix: %q", got[2].Input)
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

func TestRcfPullClause(t *testing.T) {
	t.Parallel()
	if got := rcfPullClause("default"); got != "pull" {
		t.Fatalf("default → %q", got)
	}
	if got := rcfPullClause("FOO"); got != "pull unit FOO" {
		t.Fatalf("named → %q", got)
	}
}

func TestParseRcfSection_SourcePulledFrom(t *testing.T) {
	t.Parallel()
	out := strings.Join([]string{
		"router general",
		"   control-functions",
		"      code source pulled-from flash:default.txt",
		"      code unit EVPN_RCF source pulled-from flash:rcf-evpn.txt",
		"      code unit OTHER source pulled-from flash:other.txt edited",
	}, "\n")

	defaultRow, ok := parseRcfSection(out, "default")
	if !ok || defaultRow.SourceFile != "flash:default.txt" || defaultRow.Code != "" {
		t.Fatalf("default row: %+v ok=%v", defaultRow, ok)
	}
	evpnRow, ok := parseRcfSection(out, "EVPN_RCF")
	if !ok || evpnRow.SourceFile != "flash:rcf-evpn.txt" {
		t.Fatalf("EVPN_RCF row: %+v ok=%v", evpnRow, ok)
	}
	editedRow, ok := parseRcfSection(out, "OTHER")
	if !ok || editedRow.SourceFile != "flash:other.txt" {
		t.Fatalf("OTHER edited-suffix not stripped: %+v", editedRow)
	}
	if _, ok := parseRcfSection(out, "MISSING"); ok {
		t.Fatal("must not match unknown name")
	}
}

func TestParseRcfSection_InlineCode(t *testing.T) {
	t.Parallel()
	out := strings.Join([]string{
		"router general",
		"   control-functions",
		"      code unit EVPN_RCF",
		"         function ACCEPT_ALL() {",
		"           return true;",
		"         }",
		"         EOF",
		"      code unit OTHER",
		"         function DENY_ALL() { return false; }",
		"         EOF",
	}, "\n")

	row, ok := parseRcfSection(out, "EVPN_RCF")
	if !ok {
		t.Fatal("EVPN_RCF unit not found")
	}
	if row.SourceFile != "" {
		t.Fatalf("SourceFile should be empty for inline form: %q", row.SourceFile)
	}
	if !strings.Contains(row.Code, "function ACCEPT_ALL()") || !strings.Contains(row.Code, "return true;") {
		t.Fatalf("Code body missing expected lines: %q", row.Code)
	}

	otherRow, ok := parseRcfSection(out, "OTHER")
	if !ok || !strings.Contains(otherRow.Code, "DENY_ALL") {
		t.Fatalf("OTHER row: %+v ok=%v", otherRow, ok)
	}
}

func TestLeadingWhitespace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, out string
	}{
		{"   foo", "   "},
		{"\tfoo", "\t"},
		{"foo", ""},
		{"", ""},
		{"  \t  bar", "  \t  "},
	}
	for _, tc := range tests {
		if got := leadingWhitespace(tc.in); got != tc.out {
			t.Fatalf("leadingWhitespace(%q) = %q, want %q", tc.in, got, tc.out)
		}
	}
}
