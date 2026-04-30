package l3

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateCommunityList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args CommunityListArgs
		want error
	}{
		{
			name: "ok_standard",
			args: CommunityListArgs{
				Name:    "L",
				Entries: []CommunityListEntry{{Action: "permit", Value: "65000:100"}},
			},
		},
		{
			name: "ok_regexp",
			args: CommunityListArgs{
				Name:    "L",
				Type:    new("regexp"),
				Entries: []CommunityListEntry{{Action: "permit", Value: "^65000:.*$"}},
			},
		},
		{
			name: "empty_name",
			args: CommunityListArgs{Entries: []CommunityListEntry{{Action: "permit", Value: "65000:1"}}},
			want: ErrCommunityListNameRequired,
		},
		{
			name: "bad_type",
			args: CommunityListArgs{
				Name:    "L",
				Type:    new("expanded"),
				Entries: []CommunityListEntry{{Action: "permit", Value: "65000:1"}},
			},
			want: ErrCommunityListTypeInvalid,
		},
		{
			name: "empty_entries",
			args: CommunityListArgs{Name: "L"},
			want: ErrCommunityListEmptyEntries,
		},
		{
			name: "bad_action",
			args: CommunityListArgs{
				Name:    "L",
				Entries: []CommunityListEntry{{Action: "log", Value: "65000:1"}},
			},
			want: ErrCommunityListAction,
		},
		{
			name: "empty_value",
			args: CommunityListArgs{
				Name:    "L",
				Entries: []CommunityListEntry{{Action: "permit", Value: ""}},
			},
			want: ErrCommunityListValueEmpty,
		},
		{
			name: "bad_standard_value",
			args: CommunityListArgs{
				Name:    "L",
				Entries: []CommunityListEntry{{Action: "permit", Value: "not-a-community"}},
			},
			want: ErrCommunityListValueStd,
		},
		{
			name: "regexp_accepts_anything",
			args: CommunityListArgs{
				Name:    "L",
				Type:    new("regexp"),
				Entries: []CommunityListEntry{{Action: "permit", Value: "anything-goes-here"}},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateCommunityList(tc.args)
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

func TestValidCommunityStandardValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in string
		ok bool
	}{
		{"internet", true},
		{"local-as", true},
		{"no-advertise", true},
		{"no-export", true},
		{"GSHUT", true},
		{"65000:100", true},
		{"0:0", true},
		{"65535:65535", true},
		{"100", true},
		{"4294967040", true},
		{"4294967041", false},
		{"65536:1", false},
		{"1:65536", false},
		{"random", false},
		{":1", false},
		{"abc:def", false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := validCommunityStandardValue(tc.in); got != tc.ok {
				t.Fatalf("validCommunityStandardValue(%q) = %v, want %v", tc.in, got, tc.ok)
			}
		})
	}
}

func TestBuildCommunityListCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		args   CommunityListArgs
		remove bool
		want   []string
	}{
		{
			name: "standard",
			args: CommunityListArgs{
				Name: "STD",
				Entries: []CommunityListEntry{
					{Action: "permit", Value: "65000:100"},
					{Action: "permit", Value: "no-export"},
					{Action: "deny", Value: "65001:200"},
				},
			},
			want: []string{
				"no ip community-list STD",
				"ip community-list STD permit 65000:100",
				"ip community-list STD permit no-export",
				"ip community-list STD deny 65001:200",
			},
		},
		{
			name: "regexp",
			args: CommunityListArgs{
				Name: "EXP",
				Type: new("regexp"),
				Entries: []CommunityListEntry{
					{Action: "permit", Value: "^65000:.*$"},
					{Action: "deny", Value: ".*:666$"},
				},
			},
			want: []string{
				"no ip community-list EXP",
				"ip community-list regexp EXP permit ^65000:.*$",
				"ip community-list regexp EXP deny .*:666$",
			},
		},
		{
			name:   "delete",
			args:   CommunityListArgs{Name: "STD"},
			remove: true,
			want:   []string{"no ip community-list STD"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildCommunityListCmds(tc.args, tc.remove)
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

func TestCommunityListID(t *testing.T) {
	t.Parallel()
	if got := communityListID("STD"); got != "community-list/STD" {
		t.Fatalf("got %q", got)
	}
}

func TestParseCommunityListLines(t *testing.T) {
	t.Parallel()
	out := strings.Join([]string{
		"ip community-list STD permit 65000:100",
		"ip community-list STD permit no-export",
		"ip community-list STD deny 65001:200",
		"ip community-list regexp EXP permit ^65000:.*$",
		"ip community-list OTHER permit 1:1",
	}, "\n")

	stdRow, ok := parseCommunityListLines(out, "STD")
	if !ok {
		t.Fatal("expected STD found")
	}
	if stdRow.Type != "standard" {
		t.Fatalf("STD type = %q", stdRow.Type)
	}
	if len(stdRow.Entries) != 3 {
		t.Fatalf("STD entries count = %d", len(stdRow.Entries))
	}
	if stdRow.Entries[0].Value != "65000:100" {
		t.Fatalf("STD entry[0] value = %q", stdRow.Entries[0].Value)
	}
	if stdRow.Entries[2].Action != "deny" {
		t.Fatalf("STD entry[2] action = %q", stdRow.Entries[2].Action)
	}

	expRow, ok := parseCommunityListLines(out, "EXP")
	if !ok {
		t.Fatal("expected EXP found")
	}
	if expRow.Type != "regexp" {
		t.Fatalf("EXP type = %q", expRow.Type)
	}
	if len(expRow.Entries) != 1 || expRow.Entries[0].Value != "^65000:.*$" {
		t.Fatalf("EXP entries: %+v", expRow.Entries)
	}

	if _, ok := parseCommunityListLines(out, "MISSING"); ok {
		t.Fatal("must not match unknown name")
	}
}
