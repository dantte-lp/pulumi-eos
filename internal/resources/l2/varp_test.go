package l2

import (
	"errors"
	"reflect"
	"testing"
)

func TestValidateVarp(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args VarpArgs
		want error
	}{
		{name: "missing", args: VarpArgs{}, want: ErrVarpMacRequired},
		{name: "blank", args: VarpArgs{MacAddress: "   "}, want: ErrVarpMacRequired},
		{name: "garbage", args: VarpArgs{MacAddress: "not-a-mac"}, want: ErrVarpMacBadFormat},
		{name: "multicast first octet", args: VarpArgs{MacAddress: "01:00:5e:00:00:01"}, want: ErrVarpMacMulticast},
		{name: "valid colon", args: VarpArgs{MacAddress: "00:1c:73:00:00:01"}},
		{name: "valid hyphen", args: VarpArgs{MacAddress: "00-1c-73-00-00-01"}},
		{name: "valid cisco", args: VarpArgs{MacAddress: "001c.7300.0001"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateVarp(tc.args)
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

func TestNormalizeMac(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{"00:1c:73:00:00:01", "00:1c:73:00:00:01"},
		{"00-1C-73-00-00-01", "00:1c:73:00:00:01"},
		{"001c.7300.0001", "00:1c:73:00:00:01"},
		{"00:AA:00:BB:00:CC", "00:aa:00:bb:00:cc"},
	}
	for _, tc := range cases {
		got, err := normalizeMac(tc.in)
		if err != nil {
			t.Errorf("normalizeMac(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("normalizeMac(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildVarpCmds(t *testing.T) {
	t.Parallel()

	got, err := buildVarpCmds(VarpArgs{MacAddress: "001c.7300.0001"}, false)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	want := []string{"ip virtual-router mac-address 00:1c:73:00:00:01"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands mismatch: want %v, got %v", want, got)
	}
}

func TestBuildVarpCmds_Remove(t *testing.T) {
	t.Parallel()

	got, err := buildVarpCmds(VarpArgs{MacAddress: "001c.7300.0001"}, true)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	want := []string{"no ip virtual-router mac-address 00:1c:73:00:00:01"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands mismatch: want %v, got %v", want, got)
	}
}

func TestParseVarpConfig(t *testing.T) {
	t.Parallel()

	mac, found, err := parseVarpConfig("ip virtual-router mac-address 00:1c:73:00:00:01\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if mac != "00:1c:73:00:00:01" {
		t.Errorf("mac: got %q", mac)
	}
}

func TestParseVarpConfig_Absent(t *testing.T) {
	t.Parallel()

	_, found, err := parseVarpConfig("hostname switch1\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if found {
		t.Fatal("expected found=false")
	}
}
