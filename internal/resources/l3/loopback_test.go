package l3

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateLoopback_NumberRange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    LoopbackArgs
		wantErr error
	}{
		{"min", LoopbackArgs{Number: 0, IPAddress: new("10.0.0.1/32")}, nil},
		{"mid", LoopbackArgs{Number: 100, IPAddress: new("10.0.0.1/32")}, nil},
		{"max", LoopbackArgs{Number: 1000, IPAddress: new("10.0.0.1/32")}, nil},
		{"negative", LoopbackArgs{Number: -1, IPAddress: new("10.0.0.1/32")}, ErrLoopbackNumberOutOfRange},
		{"too-big", LoopbackArgs{Number: 1001, IPAddress: new("10.0.0.1/32")}, ErrLoopbackNumberOutOfRange},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateLoopback(tc.args)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestValidateLoopback_IPAddress(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    LoopbackArgs
		wantErr error
	}{
		{"v4_only", LoopbackArgs{Number: 0, IPAddress: new("10.0.0.1/32")}, nil},
		{"v6_only", LoopbackArgs{Number: 0, IPv6Address: new("fc00::1/128")}, nil},
		{"both", LoopbackArgs{Number: 0, IPAddress: new("10.0.0.1/32"), IPv6Address: new("fc00::1/128")}, nil},
		{"none", LoopbackArgs{Number: 0}, ErrLoopbackNoAddress},
		{"empty_v4", LoopbackArgs{Number: 0, IPAddress: new("")}, ErrLoopbackNoAddress},
		{"bad_v4_no_mask", LoopbackArgs{Number: 0, IPAddress: new("10.0.0.1")}, ErrLoopbackBadIPv4},
		{"bad_v4_garbage", LoopbackArgs{Number: 0, IPAddress: new("not.an.ip/32")}, ErrLoopbackBadIPv4},
		{"v6_in_v4_field", LoopbackArgs{Number: 0, IPAddress: new("fc00::1/128")}, ErrLoopbackBadIPv4},
		{"bad_v6_no_mask", LoopbackArgs{Number: 0, IPv6Address: new("fc00::1")}, ErrLoopbackBadIPv6},
		{"v4_in_v6_field", LoopbackArgs{Number: 0, IPv6Address: new("10.0.0.1/32")}, ErrLoopbackBadIPv6},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateLoopback(tc.args)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestBuildLoopbackCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		args   LoopbackArgs
		remove bool
		want   []string
	}{
		{
			name: "create_minimal_v4",
			args: LoopbackArgs{Number: 0, IPAddress: new("10.0.0.1/32")},
			want: []string{"interface Loopback0", "ip address 10.0.0.1/32", "no shutdown"},
		},
		{
			name: "create_dual_stack_with_vrf_and_desc",
			args: LoopbackArgs{
				Number:      1,
				IPAddress:   new("10.0.0.2/32"),
				IPv6Address: new("fc00::2/128"),
				Vrf:         new("OVERLAY"),
				Description: new("VTEP source"),
			},
			want: []string{
				"interface Loopback1",
				"description VTEP source",
				"vrf OVERLAY",
				"ip address 10.0.0.2/32",
				"ipv6 address fc00::2/128",
				"no shutdown",
			},
		},
		{
			name: "create_shutdown",
			args: LoopbackArgs{Number: 9, IPAddress: new("10.9.9.9/32"), Shutdown: new(true)},
			want: []string{"interface Loopback9", "ip address 10.9.9.9/32", "shutdown"},
		},
		{
			name:   "delete",
			args:   LoopbackArgs{Number: 5},
			remove: true,
			want:   []string{"no interface Loopback5"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildLoopbackCmds(tc.args, tc.remove)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %d (%v), want %d (%v)", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("cmd[%d]: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestLoopbackID(t *testing.T) {
	t.Parallel()
	if got := loopbackID(0); got != "loopback/0" {
		t.Fatalf("got %q, want loopback/0", got)
	}
	if got := loopbackName(42); got != "Loopback42" {
		t.Fatalf("got %q, want Loopback42", got)
	}
}

func TestParseLoopbackConfig(t *testing.T) {
	t.Parallel()
	out := strings.Join([]string{
		"interface Loopback1",
		"   description VTEP source",
		"   vrf OVERLAY",
		"   ip address 10.0.0.2/32",
		"   ipv6 address fc00::2/128",
		"   shutdown",
		"!",
	}, "\n")
	row, found, err := parseLoopbackConfig(out, 1)
	if err != nil || !found {
		t.Fatalf("parse: found=%v err=%v", found, err)
	}
	if row.Description != "VTEP source" {
		t.Fatalf("description = %q", row.Description)
	}
	if row.Vrf != "OVERLAY" {
		t.Fatalf("vrf = %q", row.Vrf)
	}
	if row.Address != "10.0.0.2/32" {
		t.Fatalf("address = %q", row.Address)
	}
	if row.IPv6Address != "fc00::2/128" {
		t.Fatalf("ipv6 = %q", row.IPv6Address)
	}
	if !row.Shutdown {
		t.Fatalf("shutdown should be true")
	}
}

func TestParseLoopbackConfig_Missing(t *testing.T) {
	t.Parallel()
	_, found, err := parseLoopbackConfig("", 0)
	if err != nil || found {
		t.Fatalf("empty input should yield (false, nil); got found=%v err=%v", found, err)
	}
	_, found, err = parseLoopbackConfig("interface Loopback99\n", 0)
	if err != nil || found {
		t.Fatalf("wrong header should yield (false, nil); got found=%v err=%v", found, err)
	}
}
