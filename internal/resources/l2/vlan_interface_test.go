package l2

import (
	"errors"
	"testing"
)

func TestValidateVlanInterface(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    VlanInterfaceArgs
		wantErr error
	}{
		{
			name: "ok_minimal",
			args: VlanInterfaceArgs{VlanId: 100},
		},
		{
			name: "ok_with_address",
			args: VlanInterfaceArgs{VlanId: 800, IpAddress: new("10.0.0.1/24")},
		},
		{
			name: "ok_with_virtual",
			args: VlanInterfaceArgs{VlanId: 800, IpAddressVirtual: new("10.0.0.1/24")},
		},
		{
			name:    "address_conflict",
			args:    VlanInterfaceArgs{VlanId: 800, IpAddress: new("10.0.0.1/24"), IpAddressVirtual: new("10.0.0.1/24")},
			wantErr: ErrVlanInterfaceAddrConflict,
		},
		{
			name:    "vlan_out_of_range",
			args:    VlanInterfaceArgs{VlanId: 0},
			wantErr: ErrVlanIDOutOfRange,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateVlanInterface(tc.args)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestBuildVlanInterfaceCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		args     VlanInterfaceArgs
		remove   bool
		expected []string
	}{
		{
			name: "create_with_virtual_and_vrf",
			args: VlanInterfaceArgs{
				VlanId:           800,
				Vrf:              new("VRF-PROD"),
				IpAddressVirtual: new("10.32.0.1/24"),
				Mtu:              new(9000),
				Description:      new("prod fronts"),
				NoAutostate:      new(true),
			},
			expected: []string{
				"interface Vlan800",
				"description prod fronts",
				"vrf VRF-PROD",
				"ip address virtual 10.32.0.1/24",
				"mtu 9000",
				"no autostate",
			},
		},
		{
			name: "create_minimal_static_address",
			args: VlanInterfaceArgs{
				VlanId:    100,
				IpAddress: new("192.168.0.1/24"),
			},
			expected: []string{
				"interface Vlan100",
				"ip address 192.168.0.1/24",
			},
		},
		{
			name:     "delete",
			args:     VlanInterfaceArgs{VlanId: 999},
			remove:   true,
			expected: []string{"no interface Vlan999"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildVlanInterfaceCmds(tc.args, tc.remove)
			if len(got) != len(tc.expected) {
				t.Fatalf("len mismatch: got %d (%v), want %d (%v)", len(got), got, len(tc.expected), tc.expected)
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Fatalf("cmd[%d]: got %q, want %q", i, got[i], tc.expected[i])
				}
			}
		})
	}
}

func TestParseVlanInterfaceConfig(t *testing.T) {
	t.Parallel()
	cfg := `interface Vlan800
   description prod fronts
   vrf VRF-PROD
   ip address virtual 10.32.0.1/24
   mtu 9000
   no autostate
!
`
	row, ok, err := parseVlanInterfaceConfig(cfg, 800)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if row.Description != "prod fronts" {
		t.Errorf("description: got %q", row.Description)
	}
	if row.Vrf != "VRF-PROD" {
		t.Errorf("vrf: got %q", row.Vrf)
	}
	if row.Virtual != "10.32.0.1/24" {
		t.Errorf("virtual: got %q", row.Virtual)
	}
	if row.Address != "" {
		t.Errorf("address must be empty when virtual is set: got %q", row.Address)
	}
	if row.Mtu != 9000 {
		t.Errorf("mtu: got %d", row.Mtu)
	}
	if !row.NoAutostate {
		t.Errorf("noAutostate must be true")
	}

	if _, ok, _ := parseVlanInterfaceConfig("interface Vlan100\n", 999); ok {
		t.Errorf("absent SVI: got ok=true, want false")
	}
}
