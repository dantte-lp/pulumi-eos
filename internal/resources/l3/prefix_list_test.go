package l3

import (
	"errors"
	"strings"
	"testing"
)

func TestValidatePrefixList_TopLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args PrefixListArgs
		want error
	}{
		{
			name: "ok",
			args: PrefixListArgs{Name: "L", Entries: []PrefixListEntry{{Seq: 10, Action: "permit", Prefix: "10.0.0.0/8"}}},
		},
		{"empty_name", PrefixListArgs{Name: "", Entries: []PrefixListEntry{{Seq: 10, Action: "permit", Prefix: "10.0.0.0/8"}}}, ErrPrefixListNameRequired},
		{"whitespace_name", PrefixListArgs{Name: "  ", Entries: []PrefixListEntry{{Seq: 10, Action: "permit", Prefix: "10.0.0.0/8"}}}, ErrPrefixListNameRequired},
		{"empty_entries", PrefixListArgs{Name: "L"}, ErrPrefixListEntriesEmpty},
		{
			name: "duplicate_seq",
			args: PrefixListArgs{
				Name: "L",
				Entries: []PrefixListEntry{
					{Seq: 10, Action: "permit", Prefix: "10.0.0.0/8"},
					{Seq: 10, Action: "deny", Prefix: "192.0.2.0/24"},
				},
			},
			want: ErrPrefixListSeqDuplicate,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validatePrefixList(tc.args)
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

func TestValidatePrefixListEntry(t *testing.T) {
	t.Parallel()
	base := PrefixListEntry{Seq: 10, Action: "permit", Prefix: "10.0.0.0/8"}
	tests := []struct {
		name string
		mut  func(PrefixListEntry) PrefixListEntry
		want error
	}{
		{"ok_minimal", func(e PrefixListEntry) PrefixListEntry { return e }, nil},
		{"seq_negative", func(e PrefixListEntry) PrefixListEntry { e.Seq = -1; return e }, ErrPrefixListSeqOutOfRange},
		{"seq_high", func(e PrefixListEntry) PrefixListEntry { e.Seq = 65536; return e }, ErrPrefixListSeqOutOfRange},
		{"action_garbage", func(e PrefixListEntry) PrefixListEntry { e.Action = "log"; return e }, ErrPrefixListActionInvalid},
		{"prefix_v6", func(e PrefixListEntry) PrefixListEntry { e.Prefix = "fc00::/64"; return e }, ErrPrefixListBadPrefix},
		{"prefix_no_mask", func(e PrefixListEntry) PrefixListEntry { e.Prefix = "10.0.0.0"; return e }, ErrPrefixListBadPrefix},
		{"eq_only", func(e PrefixListEntry) PrefixListEntry { e.Eq = new(24); return e }, nil},
		{"ge_le", func(e PrefixListEntry) PrefixListEntry { e.Ge = new(16); e.Le = new(24); return e }, nil},
		{"eq_with_ge", func(e PrefixListEntry) PrefixListEntry { e.Eq = new(24); e.Ge = new(16); return e }, ErrPrefixListMaskCombo},
		{"eq_with_le", func(e PrefixListEntry) PrefixListEntry { e.Eq = new(24); e.Le = new(24); return e }, ErrPrefixListMaskCombo},
		{"mask_zero", func(e PrefixListEntry) PrefixListEntry { e.Eq = new(0); return e }, ErrPrefixListMaskOutOfRange},
		{"mask_high", func(e PrefixListEntry) PrefixListEntry { e.Eq = new(33); return e }, ErrPrefixListMaskOutOfRange},
		{"ge_gt_le", func(e PrefixListEntry) PrefixListEntry { e.Ge = new(24); e.Le = new(16); return e }, ErrPrefixListGeLeOrder},
		{"ge_below_pfx", func(e PrefixListEntry) PrefixListEntry { e.Ge = new(4); return e }, ErrPrefixListGeAtLeastPfx},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := tc.mut(base)
			err := validatePrefixListEntry(&e)
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

func TestBuildPrefixListCmds(t *testing.T) {
	t.Parallel()
	args := PrefixListArgs{
		Name:   "ALPHA",
		Remark: new("test remark"),
		Entries: []PrefixListEntry{
			// Out of order on input — render must sort by Seq.
			{Seq: 30, Action: "permit", Prefix: "192.0.2.0/24"},
			{Seq: 10, Action: "permit", Prefix: "10.0.0.0/8", Ge: new(16), Le: new(24)},
			{Seq: 20, Action: "deny", Prefix: "0.0.0.0/0", Ge: new(25)},
			{Seq: 40, Action: "permit", Prefix: "172.16.0.0/12", Eq: new(16)},
		},
	}
	got := buildPrefixListCmds(args, false)
	want := []string{
		"no ip prefix-list ALPHA",
		"ip prefix-list ALPHA remark test remark",
		"ip prefix-list ALPHA seq 10 permit 10.0.0.0/8 ge 16 le 24",
		"ip prefix-list ALPHA seq 20 deny 0.0.0.0/0 ge 25",
		"ip prefix-list ALPHA seq 30 permit 192.0.2.0/24",
		"ip prefix-list ALPHA seq 40 permit 172.16.0.0/12 eq 16",
	}
	if len(got) != len(want) {
		t.Fatalf("len mismatch:\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("cmd[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildPrefixListCmds_Delete(t *testing.T) {
	t.Parallel()
	got := buildPrefixListCmds(PrefixListArgs{Name: "ALPHA"}, true)
	want := []string{"no ip prefix-list ALPHA"}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPrefixListID(t *testing.T) {
	t.Parallel()
	if got := prefixListID("ALPHA"); got != "prefix-list/ALPHA" {
		t.Fatalf("got %q", got)
	}
}

func TestSanitizePrefixListName(t *testing.T) {
	t.Parallel()
	if got := sanitizePrefixListName("foo.bar:1"); got != "foo-bar-1" {
		t.Fatalf("got %q", got)
	}
}

func TestParsePrefixListLines(t *testing.T) {
	t.Parallel()
	out := strings.Join([]string{
		"ip prefix-list ALPHA remark test",
		"ip prefix-list ALPHA seq 10 permit 10.0.0.0/8 ge 16 le 24",
		"ip prefix-list ALPHA seq 20 deny 0.0.0.0/0 ge 25",
		"ip prefix-list BETA seq 5 permit 192.0.2.0/24",
	}, "\n")

	row, found := parsePrefixListLines(out, "ALPHA")
	if !found {
		t.Fatal("expected ALPHA found")
	}
	if row.Remark != "test" {
		t.Fatalf("remark = %q", row.Remark)
	}
	if len(row.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(row.Entries))
	}
	if row.Entries[0].Seq != 10 || row.Entries[0].Action != "permit" || row.Entries[0].Prefix != "10.0.0.0/8" {
		t.Fatalf("entry[0] = %+v", row.Entries[0])
	}
	if row.Entries[0].Ge == nil || *row.Entries[0].Ge != 16 || row.Entries[0].Le == nil || *row.Entries[0].Le != 24 {
		t.Fatalf("entry[0] ge/le mismatch: %+v", row.Entries[0])
	}

	rowB, found := parsePrefixListLines(out, "BETA")
	if !found {
		t.Fatal("expected BETA found")
	}
	if len(rowB.Entries) != 1 || rowB.Entries[0].Seq != 5 {
		t.Fatalf("BETA mismatch: %+v", rowB)
	}

	if _, found := parsePrefixListLines(out, "GAMMA"); found {
		t.Fatal("must not match unknown name")
	}
}

func TestParsePrefixListEntryRest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		rest string
		want PrefixListEntry
		ok   bool
	}{
		{"minimal", "seq 10 permit 10.0.0.0/8", PrefixListEntry{Seq: 10, Action: "permit", Prefix: "10.0.0.0/8"}, true},
		{"with_ge_le", "seq 20 deny 0.0.0.0/0 ge 25 le 32", PrefixListEntry{Seq: 20, Action: "deny", Prefix: "0.0.0.0/0", Ge: new(25), Le: new(32)}, true},
		{"with_eq", "seq 30 permit 172.16.0.0/12 eq 16", PrefixListEntry{Seq: 30, Action: "permit", Prefix: "172.16.0.0/12", Eq: new(16)}, true},
		{"missing_seq", "permit 10.0.0.0/8", PrefixListEntry{}, false},
		{"bad_action", "seq 10 log 10.0.0.0/8", PrefixListEntry{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parsePrefixListEntryRest(tc.rest)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (got=%+v)", ok, tc.ok, got)
			}
			if !ok {
				return
			}
			if got.Seq != tc.want.Seq || got.Action != tc.want.Action || got.Prefix != tc.want.Prefix {
				t.Fatalf("base mismatch: got %+v want %+v", got, tc.want)
			}
		})
	}
}
