package l2

import (
	"errors"
	"testing"
)

func TestValidateInterface(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    InterfaceArgs
		wantErr error
	}{
		{
			name: "ok_minimal",
			args: InterfaceArgs{Name: "Ethernet1"},
		},
		{
			name: "ok_access",
			args: InterfaceArgs{Name: "Ethernet1", SwitchportMode: new(SwitchportModeAccess), AccessVlan: new(800)},
		},
		{
			name: "ok_trunk",
			args: InterfaceArgs{
				Name:              "Ethernet2",
				SwitchportMode:    new(SwitchportModeTrunk),
				TrunkAllowedVlans: new("1-100"),
				TrunkNativeVlan:   new(99),
			},
		},
		{
			name: "ok_routed",
			args: InterfaceArgs{Name: "Ethernet3", SwitchportMode: new(SwitchportModeRouted)},
		},
		{
			name: "ok_lacp",
			args: InterfaceArgs{
				Name:         "Ethernet4",
				ChannelGroup: &ChannelGroup{Id: 10, Mode: ChannelGroupModeActive},
			},
		},
		{
			name:    "missing_name",
			args:    InterfaceArgs{Name: "  "},
			wantErr: ErrInterfaceNameRequired,
		},
		{
			name:    "bad_switchport_mode",
			args:    InterfaceArgs{Name: "Ethernet1", SwitchportMode: new("monitor")},
			wantErr: ErrSwitchportModeInvalid,
		},
		{
			name:    "access_vlan_on_trunk",
			args:    InterfaceArgs{Name: "Ethernet1", SwitchportMode: new(SwitchportModeTrunk), AccessVlan: new(100)},
			wantErr: ErrSwitchportAccessOnTrunk,
		},
		{
			name:    "trunk_field_on_access",
			args:    InterfaceArgs{Name: "Ethernet1", SwitchportMode: new(SwitchportModeAccess), TrunkAllowedVlans: new("1-100")},
			wantErr: ErrSwitchportTrunkOnAccess,
		},
		{
			name:    "bad_channel_group_mode",
			args:    InterfaceArgs{Name: "Ethernet1", ChannelGroup: &ChannelGroup{Id: 1, Mode: "auto"}},
			wantErr: ErrInterfaceCgModeInvalid,
		},
		{
			name: "bad_vlan_range",
			args: InterfaceArgs{Name: "Ethernet1", SwitchportMode: new(SwitchportModeAccess), AccessVlan: new(0)},
			// returns vlan-id sentinel; we just assert non-nil here.
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateInterface(tc.args)
			if tc.wantErr == nil && tc.name != "bad_vlan_range" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if tc.name == "bad_vlan_range" {
				if err == nil {
					t.Fatal("expected vlan-range error")
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestBuildInterfaceCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		args     InterfaceArgs
		reset    bool
		expected []string
	}{
		{
			name: "create_access_with_lacp",
			args: InterfaceArgs{
				Name:           "Ethernet1",
				Description:    new("server-1 nic-0"),
				Mtu:            new(9214),
				SwitchportMode: new(SwitchportModeAccess),
				AccessVlan:     new(800),
				ChannelGroup:   &ChannelGroup{Id: 10, Mode: ChannelGroupModeActive},
				Shutdown:       new(false),
			},
			expected: []string{
				"interface Ethernet1",
				"description server-1 nic-0",
				"mtu 9214",
				"switchport",
				"switchport mode access",
				"switchport access vlan 800",
				"channel-group 10 mode active",
				"no shutdown",
			},
		},
		{
			name: "create_trunk",
			args: InterfaceArgs{
				Name:              "Ethernet2",
				SwitchportMode:    new(SwitchportModeTrunk),
				TrunkAllowedVlans: new("100,200,800-2000"),
				TrunkNativeVlan:   new(999),
			},
			expected: []string{
				"interface Ethernet2",
				"switchport",
				"switchport mode trunk",
				"switchport trunk allowed vlan 100,200,800-2000",
				"switchport trunk native vlan 999",
			},
		},
		{
			name: "create_routed",
			args: InterfaceArgs{
				Name:           "Ethernet3",
				SwitchportMode: new(SwitchportModeRouted),
				Mtu:            new(9214),
			},
			expected: []string{
				"interface Ethernet3",
				"mtu 9214",
				"no switchport",
			},
		},
		{
			name:     "reset",
			args:     InterfaceArgs{Name: "Ethernet1/1"},
			reset:    true,
			expected: []string{"default interface Ethernet1/1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildInterfaceCmds(tc.args, tc.reset)
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

func TestSanitizeForSession(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"Ethernet1":   "Ethernet1",
		"Ethernet1/1": "Ethernet1-1",
		"Et1/1/1":     "Et1-1-1",
		"a:b:c":       "a-b-c",
	}
	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeForSession(in); got != want {
				t.Fatalf("sanitize(%q) = %q, want %q", in, got, want)
			}
		})
	}
}

func TestParseInterfaceConfig(t *testing.T) {
	t.Parallel()
	cfg := `interface Ethernet1
   description server-1 nic-0
   mtu 9214
   switchport access vlan 800
   channel-group 10 mode active
!
`
	row, ok, err := parseInterfaceConfig(cfg, "Ethernet1")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if row.Description != "server-1 nic-0" {
		t.Errorf("description: got %q", row.Description)
	}
	if row.Mtu != 9214 {
		t.Errorf("mtu: got %d", row.Mtu)
	}
	if row.Switchport.AccessVlan != 800 {
		t.Errorf("access vlan: got %d", row.Switchport.AccessVlan)
	}
	if row.ChannelGroupID != 10 || row.ChannelGroupMode != "active" {
		t.Errorf("channel-group: got %d / %q", row.ChannelGroupID, row.ChannelGroupMode)
	}

	if _, ok, _ := parseInterfaceConfig("interface Ethernet1\n", "Ethernet99"); ok {
		t.Errorf("absent interface: got ok=true, want false")
	}
}
