package l2

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestValidateVxlanInterface(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args VxlanInterfaceArgs
		want error
	}{
		{
			name: "minimum valid",
			args: VxlanInterfaceArgs{Id: 1, SourceInterface: "Loopback1"},
		},
		{
			name: "id below 1",
			args: VxlanInterfaceArgs{Id: 0, SourceInterface: "Loopback1"},
			want: ErrVxlanIDOutOfRange,
		},
		{
			name: "missing source-interface",
			args: VxlanInterfaceArgs{Id: 1},
			want: ErrVxlanSourceMissing,
		},
		{
			name: "udp-port out of range",
			args: VxlanInterfaceArgs{Id: 1, SourceInterface: "Loopback1", UdpPort: new(int)},
			want: ErrVxlanUDPPortOutOfRange,
		},
		{
			name: "vlan-vni duplicate",
			args: VxlanInterfaceArgs{
				Id:              1,
				SourceInterface: "Loopback1",
				VlanVniMap: []VlanVniEntry{
					{VlanId: 100, Vni: 10100},
					{VlanId: 100, Vni: 10101},
				},
			},
			want: ErrVxlanVlanVniDuplicate,
		},
		{
			name: "vrf-vni duplicate",
			args: VxlanInterfaceArgs{
				Id:              1,
				SourceInterface: "Loopback1",
				VrfVniMap: []VrfVniEntry{
					{Vrf: "tenant-a", Vni: 50001},
					{Vrf: "tenant-a", Vni: 50002},
				},
			},
			want: ErrVxlanVrfVniDuplicate,
		},
		{
			name: "vni below range",
			args: VxlanInterfaceArgs{
				Id:              1,
				SourceInterface: "Loopback1",
				VlanVniMap:      []VlanVniEntry{{VlanId: 100, Vni: 0}},
			},
			want: ErrVxlanVniOutOfRange,
		},
		{
			name: "vni above range",
			args: VxlanInterfaceArgs{
				Id:              1,
				SourceInterface: "Loopback1",
				VlanVniMap:      []VlanVniEntry{{VlanId: 100, Vni: VniMax + 1}},
			},
			want: ErrVxlanVniOutOfRange,
		},
		{
			name: "flood-vtep not an IP",
			args: VxlanInterfaceArgs{
				Id:              1,
				SourceInterface: "Loopback1",
				FloodVteps:      []string{"not-an-ip"},
			},
			want: ErrVxlanFloodVtepNotIP,
		},
		{
			name: "valid full surface",
			args: VxlanInterfaceArgs{
				Id:              1,
				SourceInterface: "Loopback1",
				UdpPort:         new(4789),
				VlanVniMap: []VlanVniEntry{
					{VlanId: 100, Vni: 10100},
					{VlanId: 200, Vni: 10200},
				},
				VrfVniMap: []VrfVniEntry{
					{Vrf: "tenant-a", Vni: 50001},
				},
				FloodVteps: []string{"10.0.0.1", "10.0.0.2"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateVxlanInterface(tc.args)
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

func TestBuildVxlanInterfaceCmds(t *testing.T) {
	t.Parallel()

	args := VxlanInterfaceArgs{
		Id:              1,
		Description:     new("overlay-vtep"),
		SourceInterface: "Loopback1",
		UdpPort:         new(4789),
		VlanVniMap: []VlanVniEntry{
			{VlanId: 200, Vni: 10200},
			{VlanId: 100, Vni: 10100},
		},
		VrfVniMap: []VrfVniEntry{
			{Vrf: "tenant-b", Vni: 50002},
			{Vrf: "tenant-a", Vni: 50001},
		},
		FloodVteps: []string{"10.0.0.2", "10.0.0.1"},
	}

	got := buildVxlanInterfaceCmds(args, false)
	want := []string{
		"interface Vxlan1",
		"description overlay-vtep",
		"vxlan source-interface Loopback1",
		"vxlan udp-port 4789",
		"vxlan vlan 100 vni 10100",
		"vxlan vlan 200 vni 10200",
		"vxlan vrf tenant-a vni 50001",
		"vxlan vrf tenant-b vni 50002",
		"vxlan flood vtep 10.0.0.1 10.0.0.2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands mismatch:\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestBuildVxlanInterfaceCmds_Remove(t *testing.T) {
	t.Parallel()

	got := buildVxlanInterfaceCmds(VxlanInterfaceArgs{Id: 1, SourceInterface: "Loopback1"}, true)
	want := []string{"no interface Vxlan1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands mismatch: want %v, got %v", want, got)
	}
}

func TestParseVxlanInterfaceConfig(t *testing.T) {
	t.Parallel()

	out := strings.Join([]string{
		"interface Vxlan1",
		"   description overlay-vtep",
		"   vxlan source-interface Loopback1",
		"   vxlan udp-port 4789",
		"   vxlan vlan 100 vni 10100",
		"   vxlan vlan 200 vni 10200",
		"   vxlan vrf tenant-a vni 50001",
		"   vxlan flood vtep 10.0.0.1 10.0.0.2",
		"!",
	}, "\n")

	row, found, err := parseVxlanInterfaceConfig(out, 1)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if row.Description != "overlay-vtep" {
		t.Errorf("description: got %q, want overlay-vtep", row.Description)
	}
	if row.SourceInterface != "Loopback1" {
		t.Errorf("source: got %q, want Loopback1", row.SourceInterface)
	}
	if row.UdpPort != 4789 {
		t.Errorf("udp-port: got %d, want 4789", row.UdpPort)
	}
	wantVlan := []VlanVniEntry{
		{VlanId: 100, Vni: 10100},
		{VlanId: 200, Vni: 10200},
	}
	if !reflect.DeepEqual(row.VlanVni, wantVlan) {
		t.Errorf("vlan-vni: got %v, want %v", row.VlanVni, wantVlan)
	}
	wantVrf := []VrfVniEntry{{Vrf: "tenant-a", Vni: 50001}}
	if !reflect.DeepEqual(row.VrfVni, wantVrf) {
		t.Errorf("vrf-vni: got %v, want %v", row.VrfVni, wantVrf)
	}
	wantFlood := []string{"10.0.0.1", "10.0.0.2"}
	if !reflect.DeepEqual(row.FloodVteps, wantFlood) {
		t.Errorf("flood: got %v, want %v", row.FloodVteps, wantFlood)
	}
}

func TestParseVxlanInterfaceConfig_Absent(t *testing.T) {
	t.Parallel()
	_, found, err := parseVxlanInterfaceConfig("interface Vxlan2\n  vxlan source-interface Loopback1\n", 1)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if found {
		t.Fatal("expected found=false when only Vxlan2 is present")
	}
}
