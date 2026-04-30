package l2

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func validBaseMlag() MlagArgs {
	return MlagArgs{
		DomainId:       "dc1-rack1",
		LocalInterface: "Vlan4094",
		PeerLink:       "Port-Channel1000",
		PeerAddress:    "10.0.0.2",
	}
}

func TestValidateMlag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args MlagArgs
		want error
	}{
		{name: "minimum valid", args: validBaseMlag()},
		{name: "missing domain", args: MlagArgs{LocalInterface: "Vlan4094", PeerLink: "Port-Channel1000", PeerAddress: "10.0.0.2"}, want: ErrMlagDomainRequired},
		{name: "missing local-interface", args: MlagArgs{DomainId: "x", PeerLink: "Port-Channel1000", PeerAddress: "10.0.0.2"}, want: ErrMlagLocalIfaceRequired},
		{name: "missing peer-link", args: MlagArgs{DomainId: "x", LocalInterface: "Vlan4094", PeerAddress: "10.0.0.2"}, want: ErrMlagPeerLinkRequired},
		{name: "missing peer-address", args: MlagArgs{DomainId: "x", LocalInterface: "Vlan4094", PeerLink: "Port-Channel1000"}, want: ErrMlagPeerAddressRequired},
		{
			name: "peer-address not IP",
			args: func() MlagArgs { a := validBaseMlag(); a.PeerAddress = "not-an-ip"; return a }(),
			want: ErrMlagPeerAddressNotIP,
		},
		{
			name: "heartbeat not IP",
			args: func() MlagArgs { a := validBaseMlag(); a.PeerAddressHeartbeat = new("nope"); return a }(),
			want: ErrMlagHeartbeatNotIP,
		},
		{
			name: "primary-priority out of range",
			args: func() MlagArgs { a := validBaseMlag(); a.PrimaryPriority = new(40000); return a }(),
			want: ErrMlagPrimaryPriorityRange,
		},
		{
			name: "dual-primary delay non-positive",
			args: func() MlagArgs { a := validBaseMlag(); a.DualPrimaryDetectionDelay = new(0); return a }(),
			want: ErrMlagDualPrimaryDelayRange,
		},
		{
			name: "dual-primary action invalid",
			args: func() MlagArgs {
				a := validBaseMlag()
				a.DualPrimaryDetectionDelay = new(5)
				a.DualPrimaryAction = new("shutdown")
				return a
			}(),
			want: ErrMlagDualPrimaryActionInvalid,
		},
		{
			name: "dual-primary action without delay",
			args: func() MlagArgs {
				a := validBaseMlag()
				a.DualPrimaryAction = new(DualPrimaryActionErrdisable)
				return a
			}(),
			want: ErrMlagDualPrimaryActionWithDelay,
		},
		{
			name: "recovery delay negative",
			args: func() MlagArgs {
				a := validBaseMlag()
				a.DualPrimaryRecoveryDelayMlag = new(-1)
				return a
			}(),
			want: ErrMlagRecoveryDelayNegative,
		},
		{
			name: "reload delay negative",
			args: func() MlagArgs {
				a := validBaseMlag()
				a.ReloadDelayNonMlag = new(-5)
				return a
			}(),
			want: ErrMlagReloadDelayNegative,
		},
		{
			name: "valid full surface",
			args: func() MlagArgs {
				a := validBaseMlag()
				a.PeerAddressHeartbeat = new("172.30.118.190")
				a.PeerAddressHeartbeatVrf = new("MGMT")
				a.PrimaryPriority = new(11)
				a.DualPrimaryDetectionDelay = new(5)
				a.DualPrimaryAction = new(DualPrimaryActionErrdisable)
				a.DualPrimaryRecoveryDelayMlag = new(60)
				a.DualPrimaryRecoveryDelayNonMlag = new(0)
				a.ReloadDelayMlag = new(180)
				a.ReloadDelayNonMlag = new(60)
				return a
			}(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateMlag(tc.args)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestBuildMlagCmds_Full(t *testing.T) {
	t.Parallel()

	args := validBaseMlag()
	args.PeerAddressHeartbeat = new("172.30.118.190")
	args.PeerAddressHeartbeatVrf = new("MGMT")
	args.PrimaryPriority = new(11)
	args.DualPrimaryDetectionDelay = new(5)
	args.DualPrimaryAction = new(DualPrimaryActionErrdisable)
	args.DualPrimaryRecoveryDelayMlag = new(60)
	args.DualPrimaryRecoveryDelayNonMlag = new(0)
	args.ReloadDelayMlag = new(180)
	args.ReloadDelayNonMlag = new(60)

	got := buildMlagCmds(args, false)
	want := []string{
		"no mlag configuration",
		"mlag configuration",
		"domain-id dc1-rack1",
		"local-interface Vlan4094",
		"peer-link Port-Channel1000",
		"peer-address 10.0.0.2",
		"peer-address heartbeat 172.30.118.190 vrf MGMT",
		"primary-priority 11",
		"dual-primary detection delay 5 action errdisable all-interfaces",
		"dual-primary recovery delay mlag 60 non-mlag 0",
		"reload-delay mlag 180",
		"reload-delay non-mlag 60",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands mismatch:\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestBuildMlagCmds_Remove(t *testing.T) {
	t.Parallel()
	got := buildMlagCmds(validBaseMlag(), true)
	want := []string{"no mlag configuration"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands mismatch: want %v, got %v", want, got)
	}
}

func TestParseMlagConfig(t *testing.T) {
	t.Parallel()

	out := strings.Join([]string{
		"mlag configuration",
		"   domain-id dc1-rack1",
		"   local-interface Vlan4094",
		"   peer-address 10.0.0.2",
		"   peer-address heartbeat 172.30.118.190 vrf MGMT",
		"   primary-priority 11",
		"   peer-link Port-Channel1000",
		"   dual-primary detection delay 5 action errdisable all-interfaces",
		"   dual-primary recovery delay mlag 60 non-mlag 0",
		"   reload-delay mlag 180",
		"   reload-delay non-mlag 60",
		"!",
	}, "\n")

	row, found, err := parseMlagConfig(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if row.DomainId != "dc1-rack1" {
		t.Errorf("domain-id: got %q", row.DomainId)
	}
	if row.LocalInterface != "Vlan4094" {
		t.Errorf("local-interface: got %q", row.LocalInterface)
	}
	if row.PeerLink != "Port-Channel1000" {
		t.Errorf("peer-link: got %q", row.PeerLink)
	}
	if row.PeerAddress != "10.0.0.2" {
		t.Errorf("peer-address: got %q", row.PeerAddress)
	}
	if row.PeerAddressHeartbeat != "172.30.118.190" {
		t.Errorf("peer-address heartbeat: got %q", row.PeerAddressHeartbeat)
	}
	if row.PeerAddressHeartbeatVrf != "MGMT" {
		t.Errorf("peer-address heartbeat vrf: got %q", row.PeerAddressHeartbeatVrf)
	}
	if row.PrimaryPriority != 11 {
		t.Errorf("primary-priority: got %d", row.PrimaryPriority)
	}
	if row.DualPrimaryDetectionDelay != 5 {
		t.Errorf("dual-primary delay: got %d", row.DualPrimaryDetectionDelay)
	}
	if row.DualPrimaryAction != DualPrimaryActionErrdisable {
		t.Errorf("dual-primary action: got %q", row.DualPrimaryAction)
	}
	if row.DualPrimaryRecoveryDelayMlag != 60 || row.DualPrimaryRecoveryDelayNonMlag != 0 {
		t.Errorf("recovery delays: mlag=%d non-mlag=%d", row.DualPrimaryRecoveryDelayMlag, row.DualPrimaryRecoveryDelayNonMlag)
	}
	if row.ReloadDelayMlag != 180 || row.ReloadDelayNonMlag != 60 {
		t.Errorf("reload delays: mlag=%d non-mlag=%d", row.ReloadDelayMlag, row.ReloadDelayNonMlag)
	}
}

func TestParseMlagConfig_Absent(t *testing.T) {
	t.Parallel()
	_, found, err := parseMlagConfig("hostname switch1\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if found {
		t.Fatal("expected found=false when running-config has no mlag block")
	}
}
