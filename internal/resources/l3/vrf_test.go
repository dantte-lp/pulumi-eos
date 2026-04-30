package l3

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateVrf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    VrfArgs
		wantErr error
	}{
		{"ok", VrfArgs{Name: "OVERLAY"}, nil},
		{"empty", VrfArgs{Name: ""}, ErrVrfNameRequired},
		{"whitespace", VrfArgs{Name: "  \t"}, ErrVrfNameRequired},
		{"reserved_default", VrfArgs{Name: "default"}, ErrVrfNameReserved},
		{"reserved_DEFAULT", VrfArgs{Name: "DEFAULT"}, ErrVrfNameReserved},
		{"reserved_Default", VrfArgs{Name: "Default"}, ErrVrfNameReserved},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateVrf(tc.args)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestBuildVrfCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		args   VrfArgs
		remove bool
		want   []string
	}{
		{
			name: "create_minimal_defaults",
			args: VrfArgs{Name: "OVERLAY"},
			want: []string{
				"vrf instance OVERLAY",
				"no description",
				"exit",
				"ip routing vrf OVERLAY",
				"no ipv6 unicast-routing vrf OVERLAY",
			},
		},
		{
			name: "create_with_description_and_v6",
			args: VrfArgs{
				Name:        "TENANT-A",
				Description: new("Tenant A overlay"),
				IPv6Routing: new(true),
			},
			want: []string{
				"vrf instance TENANT-A",
				"description Tenant A overlay",
				"exit",
				"ip routing vrf TENANT-A",
				"ipv6 unicast-routing vrf TENANT-A",
			},
		},
		{
			name: "create_v4_disabled",
			args: VrfArgs{Name: "MGMT", IPRouting: new(false)},
			want: []string{
				"vrf instance MGMT",
				"no description",
				"exit",
				"no ip routing vrf MGMT",
				"no ipv6 unicast-routing vrf MGMT",
			},
		},
		{
			name:   "delete",
			args:   VrfArgs{Name: "OLD"},
			remove: true,
			want: []string{
				"no ip routing vrf OLD",
				"no ipv6 unicast-routing vrf OLD",
				"no vrf instance OLD",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildVrfCmds(tc.args, tc.remove)
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

func TestVrfID(t *testing.T) {
	t.Parallel()
	if got := vrfID("OVERLAY"); got != "vrf/OVERLAY" {
		t.Fatalf("got %q, want vrf/OVERLAY", got)
	}
}

func TestParseVrfSection(t *testing.T) {
	t.Parallel()
	out := strings.Join([]string{
		"vrf instance OVERLAY",
		"   description Tenant A overlay",
		"!",
	}, "\n")
	row, found := parseVrfSection(out, "OVERLAY")
	if !found {
		t.Fatal("expected found=true")
	}
	if row.Description != "Tenant A overlay" {
		t.Fatalf("description = %q", row.Description)
	}
}

func TestParseVrfSection_NoDescription(t *testing.T) {
	t.Parallel()
	out := "vrf instance MGMT\n!\n"
	row, found := parseVrfSection(out, "MGMT")
	if !found {
		t.Fatal("expected found=true even without description")
	}
	if row.Description != "" {
		t.Fatalf("description should be empty; got %q", row.Description)
	}
}

func TestParseVrfSection_Missing(t *testing.T) {
	t.Parallel()
	if _, found := parseVrfSection("", "X"); found {
		t.Fatal("empty input should yield false")
	}
	if _, found := parseVrfSection("vrf instance OTHER\n", "X"); found {
		t.Fatal("wrong name should yield false")
	}
}
