package l3

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateAsPathAccessList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args AsPathAccessListArgs
		want error
	}{
		{
			name: "ok",
			args: AsPathAccessListArgs{
				Name:    "L",
				Entries: []AsPathAccessListEntry{{Action: "permit", Regex: "^65000$"}},
			},
		},
		{
			name: "empty_name",
			args: AsPathAccessListArgs{Entries: []AsPathAccessListEntry{{Action: "permit", Regex: "^.*$"}}},
			want: ErrAsPathListNameRequired,
		},
		{"empty_entries", AsPathAccessListArgs{Name: "L"}, ErrAsPathListEmptyEntries},
		{
			name: "bad_action",
			args: AsPathAccessListArgs{
				Name:    "L",
				Entries: []AsPathAccessListEntry{{Action: "log", Regex: "^.*$"}},
			},
			want: ErrAsPathListAction,
		},
		{
			name: "empty_regex",
			args: AsPathAccessListArgs{
				Name:    "L",
				Entries: []AsPathAccessListEntry{{Action: "permit", Regex: ""}},
			},
			want: ErrAsPathListRegexEmpty,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateAsPathAccessList(tc.args)
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

func TestBuildAsPathAccessListCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		args   AsPathAccessListArgs
		remove bool
		want   []string
	}{
		{
			name: "create",
			args: AsPathAccessListArgs{
				Name: "L",
				Entries: []AsPathAccessListEntry{
					{Action: "permit", Regex: "^65000$"},
					{Action: "permit", Regex: "_65001_"},
					{Action: "deny", Regex: "^65002.*$"},
				},
			},
			want: []string{
				"no ip as-path access-list L",
				"ip as-path access-list L permit ^65000$",
				"ip as-path access-list L permit _65001_",
				"ip as-path access-list L deny ^65002.*$",
			},
		},
		{
			name:   "delete",
			args:   AsPathAccessListArgs{Name: "L"},
			remove: true,
			want:   []string{"no ip as-path access-list L"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildAsPathAccessListCmds(tc.args, tc.remove)
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

func TestAsPathAccessListID(t *testing.T) {
	t.Parallel()
	if got := asPathAccessListID("L"); got != "as-path-access-list/L" {
		t.Fatalf("got %q", got)
	}
}

func TestParseAsPathAccessListLines(t *testing.T) {
	t.Parallel()
	// Mirror the running-config form EOS emits: trailing ` any` is
	// auto-appended to every entry. The parser must strip it.
	out := strings.Join([]string{
		"ip as-path access-list L permit ^65000$ any",
		"ip as-path access-list L permit _65001_ any",
		"ip as-path access-list L deny ^65002.*$ any",
		"ip as-path access-list OTHER permit ^.*$ any",
	}, "\n")

	row, ok := parseAsPathAccessListLines(out, "L")
	if !ok {
		t.Fatal("expected L found")
	}
	if len(row.Entries) != 3 {
		t.Fatalf("entries count = %d", len(row.Entries))
	}
	if row.Entries[0].Action != "permit" || row.Entries[0].Regex != "^65000$" {
		t.Fatalf("entry[0] = %+v", row.Entries[0])
	}
	if row.Entries[2].Action != "deny" || row.Entries[2].Regex != "^65002.*$" {
		t.Fatalf("entry[2] = %+v", row.Entries[2])
	}

	if _, ok := parseAsPathAccessListLines(out, "MISSING"); ok {
		t.Fatal("must not match unknown name")
	}
}

func TestParseAsPathAccessListEntryRest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		rest string
		want AsPathAccessListEntry
		ok   bool
	}{
		{"with_any", "permit ^65000$ any", AsPathAccessListEntry{Action: "permit", Regex: "^65000$"}, true},
		{"without_any", "deny _65001_", AsPathAccessListEntry{Action: "deny", Regex: "_65001_"}, true},
		{"bad_action", "log .*", AsPathAccessListEntry{}, false},
		{"only_action", "permit", AsPathAccessListEntry{}, false},
		{"empty_regex_after_strip", "permit  any", AsPathAccessListEntry{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseAsPathAccessListEntryRest(tc.rest)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (got=%+v)", ok, tc.ok, got)
			}
			if !ok {
				return
			}
			if got.Action != tc.want.Action || got.Regex != tc.want.Regex {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
