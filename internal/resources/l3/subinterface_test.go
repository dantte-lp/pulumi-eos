package l3

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateSubinterface_Name(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		want    error
		subName string
	}{
		{"ok_eth", nil, "Ethernet1.4011"},
		{"ok_eth_breakout", nil, "Ethernet11/1.4011"},
		{"ok_pc", nil, "Port-Channel10.4011"},
		{"missing_dot", ErrSubinterfaceNameInvalid, "Ethernet1"},
		{"vlan_iface", ErrSubinterfaceNameInvalid, "Vlan100"},
		{"loopback", ErrSubinterfaceNameInvalid, "Loopback1.1"},
		{"trailing_garbage", ErrSubinterfaceNameInvalid, "Ethernet1.4011x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args := SubinterfaceArgs{Name: tc.subName, EncapsulationVlan: 100}
			err := validateSubinterface(args)
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

func TestValidateSubinterface_Vlan(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		vlan int
		want error
	}{
		{"min", 1, nil},
		{"max", 4094, nil},
		{"zero", 0, ErrSubinterfaceVlanOutOfRange},
		{"too_big", 4095, ErrSubinterfaceVlanOutOfRange},
		{"way_out", 100000, ErrSubinterfaceVlanOutOfRange},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args := SubinterfaceArgs{Name: "Ethernet1.1", EncapsulationVlan: tc.vlan}
			err := validateSubinterface(args)
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

func TestValidateSubinterface_IP(t *testing.T) {
	t.Parallel()
	base := SubinterfaceArgs{Name: "Ethernet1.1", EncapsulationVlan: 100}
	tests := []struct {
		name string
		mut  func(SubinterfaceArgs) SubinterfaceArgs
		want error
	}{
		{"v4_ok", func(a SubinterfaceArgs) SubinterfaceArgs { a.IPAddress = new("10.0.0.1/24"); return a }, nil},
		{"v4_no_mask", func(a SubinterfaceArgs) SubinterfaceArgs { a.IPAddress = new("10.0.0.1"); return a }, ErrSubinterfaceBadIPv4},
		{"v6_in_v4", func(a SubinterfaceArgs) SubinterfaceArgs { a.IPAddress = new("fc00::1/64"); return a }, ErrSubinterfaceBadIPv4},
		{"v6_ok", func(a SubinterfaceArgs) SubinterfaceArgs { a.IPv6Address = new("fc00::1/64"); return a }, nil},
		{"v6_no_mask", func(a SubinterfaceArgs) SubinterfaceArgs { a.IPv6Address = new("fc00::1"); return a }, ErrSubinterfaceBadIPv6},
		{"v4_in_v6", func(a SubinterfaceArgs) SubinterfaceArgs { a.IPv6Address = new("10.0.0.1/24"); return a }, ErrSubinterfaceBadIPv6},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateSubinterface(tc.mut(base))
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

func TestValidateSubinterface_Bfd(t *testing.T) {
	t.Parallel()
	base := SubinterfaceArgs{Name: "Ethernet1.1", EncapsulationVlan: 100}
	tests := []struct {
		name string
		bfd  *SubinterfaceBfd
		want error
	}{
		{"none", nil, nil},
		{"ok", &SubinterfaceBfd{Interval: 100, MinRx: 100, Multiplier: 3}, nil},
		{"interval_zero", &SubinterfaceBfd{Interval: 0, MinRx: 100, Multiplier: 3}, ErrSubinterfaceBfdInterval},
		{"minrx_neg", &SubinterfaceBfd{Interval: 100, MinRx: -1, Multiplier: 3}, ErrSubinterfaceBfdInterval},
		{"mult_low", &SubinterfaceBfd{Interval: 100, MinRx: 100, Multiplier: 2}, ErrSubinterfaceBfdMultiplier},
		{"mult_high", &SubinterfaceBfd{Interval: 100, MinRx: 100, Multiplier: 51}, ErrSubinterfaceBfdMultiplier},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args := base
			args.Bfd = tc.bfd
			err := validateSubinterface(args)
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

func TestBuildSubinterfaceCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		args   SubinterfaceArgs
		remove bool
		want   []string
	}{
		{
			name: "minimal",
			args: SubinterfaceArgs{Name: "Ethernet1.4011", EncapsulationVlan: 4011},
			want: []string{
				"interface Ethernet1.4011",
				"encapsulation dot1q vlan 4011",
				"no shutdown",
			},
		},
		{
			name: "full",
			args: SubinterfaceArgs{
				Name:              "Port-Channel10.4011",
				EncapsulationVlan: 4011,
				Description:       new("DCI uplink"),
				Vrf:               new("OVERLAY"),
				IPAddress:         new("10.0.0.1/30"),
				IPv6Address:       new("fc00::1/126"),
				Mtu:               new(9000),
				Bfd:               &SubinterfaceBfd{Interval: 100, MinRx: 100, Multiplier: 3},
			},
			want: []string{
				"interface Port-Channel10.4011",
				"encapsulation dot1q vlan 4011",
				"description DCI uplink",
				"vrf OVERLAY",
				"ip address 10.0.0.1/30",
				"ipv6 address fc00::1/126",
				"mtu 9000",
				"bfd interval 100 min-rx 100 multiplier 3",
				"no shutdown",
			},
		},
		{
			name: "shutdown",
			args: SubinterfaceArgs{Name: "Ethernet1.1", EncapsulationVlan: 1, Shutdown: new(true)},
			want: []string{
				"interface Ethernet1.1",
				"encapsulation dot1q vlan 1",
				"shutdown",
			},
		},
		{
			name:   "delete",
			args:   SubinterfaceArgs{Name: "Ethernet1.4011"},
			remove: true,
			want:   []string{"no interface Ethernet1.4011"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildSubinterfaceCmds(tc.args, tc.remove)
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

func TestSubinterfaceID(t *testing.T) {
	t.Parallel()
	if got := subinterfaceID("Ethernet1.4011"); got != "subinterface/Ethernet1.4011" {
		t.Fatalf("got %q", got)
	}
}

func TestSanitizeSessionName(t *testing.T) {
	t.Parallel()
	if got := sanitizeSessionName("Ethernet1.4011"); got != "Ethernet1-4011" {
		t.Fatalf("got %q", got)
	}
}

func TestParseSubinterfaceConfig(t *testing.T) {
	t.Parallel()
	out := strings.Join([]string{
		"interface Ethernet1.4011",
		"   description DCI",
		"   encapsulation dot1q vlan 4011",
		"   vrf OVERLAY",
		"   ip address 10.0.0.1/30",
		"   ipv6 address fc00::1/126",
		"   mtu 9000",
		"   bfd interval 100 min-rx 100 multiplier 3",
		"!",
	}, "\n")
	row := parseSubinterfaceConfig(out, "Ethernet1.4011")
	if row.EncapsulationVlan != 4011 {
		t.Fatalf("vlan = %d", row.EncapsulationVlan)
	}
	if row.Description != "DCI" {
		t.Fatalf("desc = %q", row.Description)
	}
	if row.Vrf != "OVERLAY" {
		t.Fatalf("vrf = %q", row.Vrf)
	}
	if row.IPAddress != "10.0.0.1/30" || row.IPv6Address != "fc00::1/126" {
		t.Fatalf("ips = %q / %q", row.IPAddress, row.IPv6Address)
	}
	if row.Mtu != 9000 {
		t.Fatalf("mtu = %d", row.Mtu)
	}
	if row.Bfd == nil || row.Bfd.Interval != 100 || row.Bfd.MinRx != 100 || row.Bfd.Multiplier != 3 {
		t.Fatalf("bfd = %+v", row.Bfd)
	}
}

func TestParseSubinterfaceConfig_Missing(t *testing.T) {
	t.Parallel()
	row := parseSubinterfaceConfig("", "Ethernet1.1")
	if row.EncapsulationVlan != 0 || row.Description != "" {
		t.Fatalf("empty input must produce zero row: %+v", row)
	}
	row = parseSubinterfaceConfig("interface Ethernet99.1\n", "Ethernet1.1")
	if row.EncapsulationVlan != 0 {
		t.Fatalf("wrong header must produce zero row: %+v", row)
	}
}
