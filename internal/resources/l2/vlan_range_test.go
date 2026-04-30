package l2

import (
	"errors"
	"reflect"
	"testing"
)

func TestValidateVlanRange(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args VlanRangeArgs
		want error
	}{
		{name: "valid", args: VlanRangeArgs{Start: 100, End: 199}},
		{name: "valid single", args: VlanRangeArgs{Start: 100, End: 100}},
		{name: "start below 1", args: VlanRangeArgs{Start: 0, End: 100}, want: ErrVlanRangeStartRange},
		{name: "start above 4094", args: VlanRangeArgs{Start: 4095, End: 4095}, want: ErrVlanRangeStartRange},
		{name: "end above 4094", args: VlanRangeArgs{Start: 100, End: 4095}, want: ErrVlanRangeEndRange},
		{name: "end below 1", args: VlanRangeArgs{Start: 1, End: 0}, want: ErrVlanRangeEndRange},
		{name: "start > end", args: VlanRangeArgs{Start: 200, End: 100}, want: ErrVlanRangeStartGtEnd},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateVlanRange(tc.args)
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

func TestBuildVlanRangeCmds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		args   VlanRangeArgs
		remove bool
		want   []string
	}{
		{
			name: "create without name",
			args: VlanRangeArgs{Start: 100, End: 199},
			want: []string{"vlan 100-199"},
		},
		{
			name: "create with name",
			args: VlanRangeArgs{Start: 100, End: 199, Name: new("service-chain")},
			want: []string{"vlan 100-199", "name service-chain"},
		},
		{
			name:   "remove",
			args:   VlanRangeArgs{Start: 100, End: 199},
			remove: true,
			want:   []string{"no vlan 100-199"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildVlanRangeCmds(tc.args, tc.remove)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("commands mismatch: want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestVlanRangeID(t *testing.T) {
	t.Parallel()

	got := vlanRangeID(100, 199)
	want := "vlan-range/100-199"
	if got != want {
		t.Errorf("id: got %q, want %q", got, want)
	}
}
