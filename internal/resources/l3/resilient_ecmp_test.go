package l3

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateResilientEcmp(t *testing.T) {
	t.Parallel()
	base := ResilientEcmpArgs{Prefix: "10.0.0.0/24", Capacity: 16, Redundancy: 2}
	tests := []struct {
		name string
		mut  func(ResilientEcmpArgs) ResilientEcmpArgs
		want error
	}{
		{"ok_ipv4_minimal", func(a ResilientEcmpArgs) ResilientEcmpArgs { return a }, nil},
		{"ok_ipv6", func(a ResilientEcmpArgs) ResilientEcmpArgs {
			a.Prefix = "2001:db8::/32"
			a.IpFamily = new("ipv6")
			return a
		}, nil},
		{"ok_explicit_ipv4", func(a ResilientEcmpArgs) ResilientEcmpArgs { a.IpFamily = new("ipv4"); return a }, nil},
		{"ok_ordered", func(a ResilientEcmpArgs) ResilientEcmpArgs { a.Ordered = new(true); return a }, nil},
		{"prefix_garbage", func(a ResilientEcmpArgs) ResilientEcmpArgs { a.Prefix = "garbage"; return a }, ErrResilientEcmpPrefixInvalid},
		{"family_mismatch", func(a ResilientEcmpArgs) ResilientEcmpArgs {
			a.Prefix = "10.0.0.0/24"
			a.IpFamily = new("ipv6")
			return a
		}, ErrResilientEcmpAFMismatch},
		{"family_bad_token", func(a ResilientEcmpArgs) ResilientEcmpArgs { a.IpFamily = new("eui64"); return a }, ErrResilientEcmpAFMismatch},
		{"capacity_zero", func(a ResilientEcmpArgs) ResilientEcmpArgs { a.Capacity = 0; return a }, ErrResilientEcmpCapacityRange},
		{"capacity_high", func(a ResilientEcmpArgs) ResilientEcmpArgs { a.Capacity = 1025; return a }, ErrResilientEcmpCapacityRange},
		{"redundancy_zero", func(a ResilientEcmpArgs) ResilientEcmpArgs { a.Redundancy = 0; return a }, ErrResilientEcmpRedundancyRange},
		{"redundancy_high", func(a ResilientEcmpArgs) ResilientEcmpArgs { a.Redundancy = 1024; return a }, ErrResilientEcmpRedundancyRange},
		{"product_exceeded", func(a ResilientEcmpArgs) ResilientEcmpArgs {
			a.Capacity = 100
			a.Redundancy = 25
			return a
		}, ErrResilientEcmpProductExceeded},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateResilientEcmp(tc.mut(base))
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

func TestBuildResilientEcmpCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		args   ResilientEcmpArgs
		family string
		remove bool
		want   []string
	}{
		{
			name:   "ipv4_basic",
			args:   ResilientEcmpArgs{Prefix: "10.0.0.0/24", Capacity: 16, Redundancy: 2},
			family: "ipv4",
			want:   []string{"ip hardware fib ecmp resilience 10.0.0.0/24 capacity 16 redundancy 2"},
		},
		{
			name:   "ipv4_ordered",
			args:   ResilientEcmpArgs{Prefix: "10.0.0.0/24", Capacity: 16, Redundancy: 2, Ordered: new(true)},
			family: "ipv4",
			want:   []string{"ip hardware fib ecmp resilience 10.0.0.0/24 capacity 16 redundancy 2 ordered"},
		},
		{
			name:   "ipv6",
			args:   ResilientEcmpArgs{Prefix: "2001:db8::/32", Capacity: 8, Redundancy: 4},
			family: "ipv6",
			want:   []string{"ipv6 hardware fib ecmp resilience 2001:db8::/32 capacity 8 redundancy 4"},
		},
		{
			name:   "delete_ipv4",
			args:   ResilientEcmpArgs{Prefix: "10.0.0.0/24"},
			family: "ipv4",
			remove: true,
			want:   []string{"no ip hardware fib ecmp resilience 10.0.0.0/24"},
		},
		{
			name:   "delete_ipv6",
			args:   ResilientEcmpArgs{Prefix: "2001:db8::/32"},
			family: "ipv6",
			remove: true,
			want:   []string{"no ipv6 hardware fib ecmp resilience 2001:db8::/32"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildResilientEcmpCmds(tc.args, tc.family, tc.remove)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %v want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("cmd[%d]: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestResilientEcmpID(t *testing.T) {
	t.Parallel()
	if got := resilientEcmpID("10.0.0.0/24", "ipv4"); got != "resilient-ecmp/ipv4/10.0.0.0/24" {
		t.Fatalf("got %q", got)
	}
	if got := resilientEcmpID("2001:db8::/32", "ipv6"); got != "resilient-ecmp/ipv6/2001:db8::/32" {
		t.Fatalf("got %q", got)
	}
}

func TestParseResilientEcmpLines(t *testing.T) {
	t.Parallel()
	out := strings.Join([]string{
		"ip hardware fib ecmp resilience 10.0.0.0/24 capacity 16 redundancy 2",
		"ip hardware fib ecmp resilience 10.0.1.0/24 capacity 8 redundancy 4 ordered",
		"ipv6 hardware fib ecmp resilience 2001:db8::/32 capacity 32 redundancy 1",
	}, "\n")

	row, ok := parseResilientEcmpLines(out, "10.0.0.0/24", "ipv4")
	if !ok || row.Capacity != 16 || row.Redundancy != 2 || row.Ordered {
		t.Fatalf("plain v4: %+v ok=%v", row, ok)
	}
	row, ok = parseResilientEcmpLines(out, "10.0.1.0/24", "ipv4")
	if !ok || !row.Ordered {
		t.Fatalf("ordered v4: %+v ok=%v", row, ok)
	}
	row, ok = parseResilientEcmpLines(out, "2001:db8::/32", "ipv6")
	if !ok || row.Capacity != 32 {
		t.Fatalf("v6: %+v ok=%v", row, ok)
	}
	if _, ok := parseResilientEcmpLines(out, "10.0.0.0/24", "ipv6"); ok {
		t.Fatal("must not cross-match families")
	}
}
