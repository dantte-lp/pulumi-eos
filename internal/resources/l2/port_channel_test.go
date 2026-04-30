package l2

import (
	"errors"
	"testing"
)

func TestValidatePortChannel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    PortChannelArgs
		wantErr error
	}{
		{
			name: "ok_minimal",
			args: PortChannelArgs{Id: 10},
		},
		{
			name: "ok_trunk_with_fallback",
			args: PortChannelArgs{
				Id:                  10,
				SwitchportMode:      new(SwitchportModeTrunk),
				TrunkAllowedVlans:   new("1-100"),
				LacpFallback:        new(LacpFallbackIndividual),
				LacpFallbackTimeout: new(50),
			},
		},
		{
			name:    "id_too_low",
			args:    PortChannelArgs{Id: 0},
			wantErr: ErrPortChannelIdRange,
		},
		{
			name:    "id_too_high",
			args:    PortChannelArgs{Id: 2001},
			wantErr: ErrPortChannelIdRange,
		},
		{
			name:    "bad_fallback",
			args:    PortChannelArgs{Id: 10, LacpFallback: new("auto")},
			wantErr: ErrLacpFallbackInvalid,
		},
		{
			name:    "bad_fallback_timeout",
			args:    PortChannelArgs{Id: 10, LacpFallbackTimeout: new(0)},
			wantErr: ErrLacpFallbackTimeoutBad,
		},
		{
			name:    "switchport_conflict",
			args:    PortChannelArgs{Id: 10, SwitchportMode: new(SwitchportModeAccess), TrunkAllowedVlans: new("1-100")},
			wantErr: ErrSwitchportTrunkOnAccess,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validatePortChannel(tc.args)
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

func TestBuildPortChannelCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		args     PortChannelArgs
		remove   bool
		expected []string
	}{
		{
			name: "create_trunk_with_fallback",
			args: PortChannelArgs{
				Id:                  10,
				Description:         new("uplink-bundle"),
				Mtu:                 new(9214),
				SwitchportMode:      new(SwitchportModeTrunk),
				TrunkAllowedVlans:   new("100-200"),
				LacpFallback:        new(LacpFallbackStatic),
				LacpFallbackTimeout: new(100),
			},
			expected: []string{
				"interface Port-Channel10",
				"description uplink-bundle",
				"mtu 9214",
				"switchport",
				"switchport mode trunk",
				"switchport trunk allowed vlan 100-200",
				"port-channel lacp fallback static",
				"port-channel lacp fallback timeout 100",
			},
		},
		{
			name:     "delete",
			args:     PortChannelArgs{Id: 999},
			remove:   true,
			expected: []string{"no interface Port-Channel999"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildPortChannelCmds(tc.args, tc.remove)
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

func TestParsePortChannelConfig(t *testing.T) {
	t.Parallel()
	cfg := `interface Port-Channel10
   description uplink-bundle
   mtu 9214
   switchport mode trunk
   switchport trunk allowed vlan 100-200
   port-channel lacp fallback static
   port-channel lacp fallback timeout 100
!
`
	row, ok, err := parsePortChannelConfig(cfg, 10)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if row.Description != "uplink-bundle" {
		t.Errorf("description: got %q", row.Description)
	}
	if row.Mtu != 9214 {
		t.Errorf("mtu: got %d", row.Mtu)
	}
	if row.Switchport.Mode != SwitchportModeTrunk {
		t.Errorf("mode: got %q", row.Switchport.Mode)
	}
	if row.Switchport.TrunkAllowedVlans != "100-200" {
		t.Errorf("allowed: got %q", row.Switchport.TrunkAllowedVlans)
	}
	if row.LacpFallback != LacpFallbackStatic {
		t.Errorf("fallback: got %q", row.LacpFallback)
	}
	if row.LacpFallbackTimeout != 100 {
		t.Errorf("fallback timeout: got %d", row.LacpFallbackTimeout)
	}

	if _, ok, _ := parsePortChannelConfig("interface Port-Channel10\n", 999); ok {
		t.Errorf("absent port-channel: got ok=true, want false")
	}
}
