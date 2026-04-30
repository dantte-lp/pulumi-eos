package l2

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestValidateEvpnEthernetSegment(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args EvpnEthernetSegmentArgs
		want error
	}{
		{
			name: "minimum valid",
			args: EvpnEthernetSegmentArgs{
				ParentInterface: "Port-Channel100",
				Identifier:      "0011:1111:1111:1111:1111",
			},
		},
		{
			name: "missing parent",
			args: EvpnEthernetSegmentArgs{Identifier: "0011:1111:1111:1111:1111"},
			want: ErrEsParentRequired,
		},
		{
			name: "missing identifier",
			args: EvpnEthernetSegmentArgs{ParentInterface: "Port-Channel100"},
			want: ErrEsIdentifierRequired,
		},
		{
			name: "identifier wrong group count",
			args: EvpnEthernetSegmentArgs{
				ParentInterface: "Port-Channel100",
				Identifier:      "0011:1111:1111:1111",
			},
			want: ErrEsIdentifierBadFormat,
		},
		{
			name: "identifier wrong group width",
			args: EvpnEthernetSegmentArgs{
				ParentInterface: "Port-Channel100",
				Identifier:      "0011:11111:1111:1111:1111",
			},
			want: ErrEsIdentifierBadFormat,
		},
		{
			name: "route-target invalid format",
			args: EvpnEthernetSegmentArgs{
				ParentInterface:   "Port-Channel100",
				Identifier:        "0011:1111:1111:1111:1111",
				RouteTargetImport: new("not-a-mac"),
			},
			want: ErrEsRouteTargetBadFormat,
		},
		{
			name: "redundancy invalid",
			args: EvpnEthernetSegmentArgs{
				ParentInterface: "Port-Channel100",
				Identifier:      "0011:1111:1111:1111:1111",
				Redundancy:      new("active-active"),
			},
			want: ErrEsRedundancyInvalid,
		},
		{
			name: "df preference without algorithm=preference",
			args: EvpnEthernetSegmentArgs{
				ParentInterface: "Port-Channel100",
				Identifier:      "0011:1111:1111:1111:1111",
				DesignatedForwarder: &DesignatedForwarder{
					Algorithm:  EsDfAlgorithmHrw,
					Preference: new(10000),
				},
			},
			want: ErrEsDfPreferenceWithAlg,
		},
		{
			name: "df preference out of range",
			args: EvpnEthernetSegmentArgs{
				ParentInterface: "Port-Channel100",
				Identifier:      "0011:1111:1111:1111:1111",
				DesignatedForwarder: &DesignatedForwarder{
					Algorithm:  EsDfAlgorithmPreference,
					Preference: new(70000),
				},
			},
			want: ErrEsDfPreferenceRange,
		},
		{
			name: "df algorithm invalid",
			args: EvpnEthernetSegmentArgs{
				ParentInterface: "Port-Channel100",
				Identifier:      "0011:1111:1111:1111:1111",
				DesignatedForwarder: &DesignatedForwarder{
					Algorithm: "random",
				},
			},
			want: ErrEsDfAlgorithmInvalid,
		},
		{
			name: "df hold-time non-positive",
			args: EvpnEthernetSegmentArgs{
				ParentInterface: "Port-Channel100",
				Identifier:      "0011:1111:1111:1111:1111",
				DesignatedForwarder: &DesignatedForwarder{
					Algorithm: EsDfAlgorithmModulus,
					HoldTime:  new(0),
				},
			},
			want: ErrEsDfHoldTimeRange,
		},
		{
			name: "valid full surface",
			args: EvpnEthernetSegmentArgs{
				ParentInterface:   "Port-Channel100",
				Identifier:        "0011:1111:1111:1111:1111",
				Redundancy:        new("single-active"),
				RouteTargetImport: new("12:34:12:34:12:34"),
				DesignatedForwarder: &DesignatedForwarder{
					Algorithm:   EsDfAlgorithmPreference,
					Preference:  new(10000),
					DontPreempt: new(true),
					HoldTime:    new(10),
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateEvpnEthernetSegment(tc.args)
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

func TestBuildEvpnEthernetSegmentCmds(t *testing.T) {
	t.Parallel()

	args := EvpnEthernetSegmentArgs{
		ParentInterface:   "Port-Channel100",
		Identifier:        "0011:1111:1111:1111:1111",
		Redundancy:        new("single-active"),
		RouteTargetImport: new("12:34:12:34:12:34"),
		DesignatedForwarder: &DesignatedForwarder{
			Algorithm:   EsDfAlgorithmPreference,
			Preference:  new(10000),
			DontPreempt: new(true),
			HoldTime:    new(10),
		},
	}

	got := buildEvpnEthernetSegmentCmds(args, false)
	want := []string{
		"interface Port-Channel100",
		"no evpn ethernet-segment",
		"evpn ethernet-segment",
		"identifier 0011:1111:1111:1111:1111",
		"redundancy single-active",
		"route-target import 12:34:12:34:12:34",
		"designated-forwarder election algorithm preference 10000 dont-preempt",
		"designated-forwarder election hold-time 10",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands mismatch:\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestBuildEvpnEthernetSegmentCmds_Remove(t *testing.T) {
	t.Parallel()

	got := buildEvpnEthernetSegmentCmds(EvpnEthernetSegmentArgs{
		ParentInterface: "Port-Channel100",
		Identifier:      "0011:1111:1111:1111:1111",
	}, true)
	want := []string{
		"interface Port-Channel100",
		"no evpn ethernet-segment",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands mismatch: want %v, got %v", want, got)
	}
}

func TestParseEvpnEthernetSegmentConfig(t *testing.T) {
	t.Parallel()

	out := strings.Join([]string{
		"interface Port-Channel100",
		"   switchport mode trunk",
		"   switchport trunk allowed vlan 100-200",
		"   evpn ethernet-segment",
		"      identifier 0011:1111:1111:1111:1111",
		"      redundancy single-active",
		"      route-target import 12:34:12:34:12:34",
		"      designated-forwarder election algorithm preference 10000 dont-preempt",
		"      designated-forwarder election hold-time 10",
		"!",
	}, "\n")

	row, found, err := parseEvpnEthernetSegmentConfig(out, "Port-Channel100")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if row.Identifier != "0011:1111:1111:1111:1111" {
		t.Errorf("identifier: got %q", row.Identifier)
	}
	if row.Redundancy != "single-active" {
		t.Errorf("redundancy: got %q", row.Redundancy)
	}
	if row.RouteTargetImport != "12:34:12:34:12:34" {
		t.Errorf("rt: got %q", row.RouteTargetImport)
	}
	if row.DfAlgorithm != "preference" {
		t.Errorf("df algo: got %q", row.DfAlgorithm)
	}
	if row.DfPreference != 10000 {
		t.Errorf("df pref: got %d", row.DfPreference)
	}
	if !row.DfDontPreempt {
		t.Errorf("df dont-preempt: got false")
	}
	if row.DfHoldTime != 10 {
		t.Errorf("df hold-time: got %d", row.DfHoldTime)
	}
}

func TestParseEvpnEthernetSegmentConfig_NoBlock(t *testing.T) {
	t.Parallel()

	out := "interface Port-Channel100\n   switchport mode trunk\n!"
	_, found, err := parseEvpnEthernetSegmentConfig(out, "Port-Channel100")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if found {
		t.Fatal("expected found=false when no evpn ethernet-segment block is present")
	}
}
