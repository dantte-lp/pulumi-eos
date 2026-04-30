package l2

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestValidateStp(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args StpArgs
		want error
	}{
		{name: "empty is valid (use defaults)", args: StpArgs{}},
		{name: "mode mstp", args: StpArgs{Mode: new(StpModeMstp)}},
		{name: "mode rstp", args: StpArgs{Mode: new(StpModeRstp)}},
		{name: "mode rapid-pvst", args: StpArgs{Mode: new(StpModeRapidPvst)}},
		{name: "mode none", args: StpArgs{Mode: new(StpModeNone)}},
		{
			name: "mode invalid",
			args: StpArgs{Mode: new("pvst+")},
			want: ErrStpModeInvalid,
		},
		{
			name: "mst revision out of range",
			args: StpArgs{Mst: &MstConfiguration{Revision: new(70000)}},
			want: ErrStpMstRevisionRange,
		},
		{
			name: "mst instance id out of range",
			args: StpArgs{Mst: &MstConfiguration{
				Instances: []MstInstance{{Id: 0, VlanRange: "100"}},
			}},
			want: ErrStpMstInstanceRange,
		},
		{
			name: "mst instance vlan-range empty",
			args: StpArgs{Mst: &MstConfiguration{
				Instances: []MstInstance{{Id: 1, VlanRange: ""}},
			}},
			want: ErrStpMstInstanceVlanEmpty,
		},
		{
			name: "mst instance dup",
			args: StpArgs{Mst: &MstConfiguration{
				Instances: []MstInstance{
					{Id: 1, VlanRange: "100"},
					{Id: 1, VlanRange: "200"},
				},
			}},
			want: ErrStpMstInstanceDup,
		},
		{
			name: "valid full surface",
			args: StpArgs{
				Mode:                     new(StpModeMstp),
				EdgePortBpduGuardDefault: new(true),
				Mst: &MstConfiguration{
					Name:     new("REGION-A"),
					Revision: new(7),
					Instances: []MstInstance{
						{Id: 1, VlanRange: "100-199"},
						{Id: 2, VlanRange: "200-299"},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateStp(tc.args)
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

func TestBuildStpCmds_Full(t *testing.T) {
	t.Parallel()

	args := StpArgs{
		Mode:                     new(StpModeMstp),
		EdgePortBpduGuardDefault: new(true),
		Mst: &MstConfiguration{
			Name:     new("REGION-A"),
			Revision: new(7),
			Instances: []MstInstance{
				{Id: 2, VlanRange: "200-299"},
				{Id: 1, VlanRange: "100-199"},
			},
		},
	}
	got := buildStpCmds(args, false)
	want := []string{
		"spanning-tree mode mstp",
		"spanning-tree edge-port bpduguard default",
		"no spanning-tree mst configuration",
		"spanning-tree mst configuration",
		"name REGION-A",
		"revision 7",
		"instance 1 vlan 100-199",
		"instance 2 vlan 200-299",
		"exit",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands mismatch:\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestBuildStpCmds_DisableBpduGuard(t *testing.T) {
	t.Parallel()

	got := buildStpCmds(StpArgs{EdgePortBpduGuardDefault: new(false)}, false)
	want := []string{"no spanning-tree edge-port bpduguard default"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands mismatch: want %v, got %v", want, got)
	}
}

func TestBuildStpCmds_Reset(t *testing.T) {
	t.Parallel()

	got := buildStpCmds(StpArgs{}, true)
	want := []string{
		"default spanning-tree mode",
		"no spanning-tree edge-port bpduguard default",
		"no spanning-tree mst configuration",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands mismatch: want %v, got %v", want, got)
	}
}

func TestParseStpConfig(t *testing.T) {
	t.Parallel()

	out := strings.Join([]string{
		"spanning-tree mode mstp",
		"spanning-tree edge-port bpduguard default",
		"!",
		"spanning-tree mst configuration",
		"   name REGION-A",
		"   revision 7",
		"   instance 1 vlan 100-199",
		"   instance 2 vlan 200-299",
		"   exit",
		"!",
	}, "\n")

	row, found, err := parseStpConfig(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if row.Mode != "mstp" {
		t.Errorf("mode: got %q", row.Mode)
	}
	if row.EdgePortBpduGuardDefault == nil || !*row.EdgePortBpduGuardDefault {
		t.Errorf("edge-port bpduguard: got %v", row.EdgePortBpduGuardDefault)
	}
	if row.Mst == nil {
		t.Fatal("expected Mst != nil")
	}
	if row.Mst.Name == nil || *row.Mst.Name != "REGION-A" {
		t.Errorf("mst name: got %v", row.Mst.Name)
	}
	if row.Mst.Revision == nil || *row.Mst.Revision != 7 {
		t.Errorf("mst revision: got %v", row.Mst.Revision)
	}
	want := []MstInstance{
		{Id: 1, VlanRange: "100-199"},
		{Id: 2, VlanRange: "200-299"},
	}
	if !reflect.DeepEqual(row.Mst.Instances, want) {
		t.Errorf("mst instances: got %v, want %v", row.Mst.Instances, want)
	}
}

func TestParseStpConfig_NoSpanningTree(t *testing.T) {
	t.Parallel()

	_, found, err := parseStpConfig("hostname switch1\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if found {
		t.Fatal("expected found=false")
	}
}
