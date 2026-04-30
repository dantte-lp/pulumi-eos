package l3

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateExtCommunityList(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args ExtCommunityListArgs
		want error
	}{
		{
			name: "ok_standard_rt",
			args: ExtCommunityListArgs{
				Name:    "L",
				Entries: []ExtCommunityListEntry{{Action: "permit", Type: new("rt"), Value: "65000:100"}},
			},
		},
		{
			name: "ok_standard_soo",
			args: ExtCommunityListArgs{
				Name:    "L",
				Entries: []ExtCommunityListEntry{{Action: "permit", Type: new("soo"), Value: "1.2.3.4:200"}},
			},
		},
		{
			name: "ok_regexp",
			args: ExtCommunityListArgs{
				Name:     "L",
				ListType: new("regexp"),
				Entries:  []ExtCommunityListEntry{{Action: "permit", Value: "RT:65000:.*"}},
			},
		},
		{"empty_name", ExtCommunityListArgs{Entries: []ExtCommunityListEntry{{Action: "permit", Type: new("rt"), Value: "1:1"}}}, ErrExtCommunityListNameRequired},
		{
			name: "bad_listtype",
			args: ExtCommunityListArgs{
				Name:     "L",
				ListType: new("expanded"),
				Entries:  []ExtCommunityListEntry{{Action: "permit", Type: new("rt"), Value: "1:1"}},
			},
			want: ErrExtCommunityListTypeInvalid,
		},
		{"empty_entries", ExtCommunityListArgs{Name: "L"}, ErrExtCommunityListEmptyEntries},
		{
			name: "bad_action",
			args: ExtCommunityListArgs{
				Name:    "L",
				Entries: []ExtCommunityListEntry{{Action: "log", Type: new("rt"), Value: "1:1"}},
			},
			want: ErrExtCommunityListAction,
		},
		{
			name: "missing_type_standard",
			args: ExtCommunityListArgs{
				Name:    "L",
				Entries: []ExtCommunityListEntry{{Action: "permit", Value: "1:1"}},
			},
			want: ErrExtCommunityListEntryType,
		},
		{
			name: "bad_type_standard",
			args: ExtCommunityListArgs{
				Name:    "L",
				Entries: []ExtCommunityListEntry{{Action: "permit", Type: new("encap"), Value: "1:1"}},
			},
			want: ErrExtCommunityListEntryType,
		},
		{
			name: "bad_value_standard",
			args: ExtCommunityListArgs{
				Name:    "L",
				Entries: []ExtCommunityListEntry{{Action: "permit", Type: new("rt"), Value: "not-a-community"}},
			},
			want: ErrExtCommunityListStdValue,
		},
		{
			name: "regexp_with_type",
			args: ExtCommunityListArgs{
				Name:     "L",
				ListType: new("regexp"),
				Entries:  []ExtCommunityListEntry{{Action: "permit", Type: new("rt"), Value: ".*"}},
			},
			want: ErrExtCommunityListRegexpHasTyp,
		},
		{
			name: "empty_value",
			args: ExtCommunityListArgs{
				Name:    "L",
				Entries: []ExtCommunityListEntry{{Action: "permit", Type: new("rt"), Value: ""}},
			},
			want: ErrExtCommunityListValueEmpty,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateExtCommunityList(tc.args)
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

func TestBuildExtCommunityListCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		args   ExtCommunityListArgs
		remove bool
		want   []string
	}{
		{
			name: "standard",
			args: ExtCommunityListArgs{
				Name: "STD",
				Entries: []ExtCommunityListEntry{
					{Action: "permit", Type: new("rt"), Value: "65000:100"},
					{Action: "permit", Type: new("soo"), Value: "1.2.3.4:200"},
					{Action: "deny", Type: new("rt"), Value: "65001:200"},
				},
			},
			want: []string{
				"no ip extcommunity-list STD",
				"ip extcommunity-list STD permit rt 65000:100",
				"ip extcommunity-list STD permit soo 1.2.3.4:200",
				"ip extcommunity-list STD deny rt 65001:200",
			},
		},
		{
			name: "regexp",
			args: ExtCommunityListArgs{
				Name:     "EXP",
				ListType: new("regexp"),
				Entries: []ExtCommunityListEntry{
					{Action: "permit", Value: "RT:65000:.*"},
					{Action: "deny", Value: ".*:666"},
				},
			},
			want: []string{
				"no ip extcommunity-list EXP",
				"ip extcommunity-list regexp EXP permit RT:65000:.*",
				"ip extcommunity-list regexp EXP deny .*:666",
			},
		},
		{
			name:   "delete",
			args:   ExtCommunityListArgs{Name: "STD"},
			remove: true,
			want:   []string{"no ip extcommunity-list STD"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildExtCommunityListCmds(tc.args, tc.remove)
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

func TestExtCommunityListID(t *testing.T) {
	t.Parallel()
	if got := extCommunityListID("STD"); got != "ext-community-list/STD" {
		t.Fatalf("got %q", got)
	}
}

func TestParseExtCommunityListLines(t *testing.T) {
	t.Parallel()
	out := strings.Join([]string{
		"ip extcommunity-list STD permit rt 65000:100",
		"ip extcommunity-list STD permit soo 1.2.3.4:200",
		"ip extcommunity-list STD deny rt 65001:200",
		"ip extcommunity-list regexp EXP permit RT:65000:.*",
		"ip extcommunity-list OTHER permit rt 1:1",
	}, "\n")

	stdRow, ok := parseExtCommunityListLines(out, "STD")
	if !ok {
		t.Fatal("expected STD found")
	}
	if stdRow.Type != "standard" {
		t.Fatalf("STD type = %q", stdRow.Type)
	}
	if len(stdRow.Entries) != 3 {
		t.Fatalf("STD entries count = %d", len(stdRow.Entries))
	}
	if stdRow.Entries[0].Type == nil || *stdRow.Entries[0].Type != "rt" || stdRow.Entries[0].Value != "65000:100" {
		t.Fatalf("STD entry[0] mismatch: %+v", stdRow.Entries[0])
	}
	if stdRow.Entries[1].Type == nil || *stdRow.Entries[1].Type != "soo" {
		t.Fatalf("STD entry[1] type mismatch: %+v", stdRow.Entries[1])
	}

	expRow, ok := parseExtCommunityListLines(out, "EXP")
	if !ok {
		t.Fatal("expected EXP found")
	}
	if expRow.Type != "regexp" {
		t.Fatalf("EXP type = %q", expRow.Type)
	}
	if len(expRow.Entries) != 1 || expRow.Entries[0].Value != "RT:65000:.*" {
		t.Fatalf("EXP entries: %+v", expRow.Entries)
	}
	if expRow.Entries[0].Type != nil {
		t.Fatalf("EXP entry must not carry a type; got %v", *expRow.Entries[0].Type)
	}

	if _, ok := parseExtCommunityListLines(out, "MISSING"); ok {
		t.Fatal("must not match unknown name")
	}
}
