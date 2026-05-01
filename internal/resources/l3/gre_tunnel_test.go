package l3

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateGreTunnel(t *testing.T) {
	t.Parallel()
	base := GreTunnelArgs{Id: 1}
	tests := []struct {
		name string
		mut  func(GreTunnelArgs) GreTunnelArgs
		want error
	}{
		{"ok_minimal", func(a GreTunnelArgs) GreTunnelArgs { return a }, nil},
		{"id_neg", func(a GreTunnelArgs) GreTunnelArgs { a.Id = -1; return a }, ErrGreTunnelIDRange},
		{"id_high", func(a GreTunnelArgs) GreTunnelArgs { a.Id = 65536; return a }, ErrGreTunnelIDRange},
		{"mode_bad", func(a GreTunnelArgs) GreTunnelArgs { a.Mode = new("vxlan"); return a }, ErrGreTunnelModeInvalid},
		{"mode_gre_ok", func(a GreTunnelArgs) GreTunnelArgs { a.Mode = new("gre"); return a }, nil},
		{"mode_mpls_gre_rejected", func(a GreTunnelArgs) GreTunnelArgs { a.Mode = new("mpls-gre"); return a }, ErrGreTunnelModeInvalid},
		{"src_bad", func(a GreTunnelArgs) GreTunnelArgs { a.Source = new("not-an-ip"); return a }, ErrGreTunnelSourceBadIPv4},
		{"src_v6", func(a GreTunnelArgs) GreTunnelArgs { a.Source = new("fc00::1"); return a }, ErrGreTunnelSourceBadIPv4},
		{"src_ok", func(a GreTunnelArgs) GreTunnelArgs { a.Source = new("10.0.0.1"); return a }, nil},
		{"dst_bad", func(a GreTunnelArgs) GreTunnelArgs { a.Destination = new("garbage"); return a }, ErrGreTunnelDestBadIPv4},
		{"tos_high", func(a GreTunnelArgs) GreTunnelArgs { a.Tos = new(256); return a }, ErrGreTunnelTosRange},
		{"tos_neg", func(a GreTunnelArgs) GreTunnelArgs { a.Tos = new(-1); return a }, ErrGreTunnelTosRange},
		{"tos_ok", func(a GreTunnelArgs) GreTunnelArgs { a.Tos = new(0); return a }, nil},
		{"key_neg", func(a GreTunnelArgs) GreTunnelArgs { a.Key = new(-1); return a }, ErrGreTunnelKeyRange},
		{"mss_zero", func(a GreTunnelArgs) GreTunnelArgs { a.MssCeiling = new(0); return a }, ErrGreTunnelMssRange},
		{"mtu_low", func(a GreTunnelArgs) GreTunnelArgs { a.Mtu = new(67); return a }, ErrGreTunnelMtuRange},
		{"mtu_high", func(a GreTunnelArgs) GreTunnelArgs { a.Mtu = new(9215); return a }, ErrGreTunnelMtuRange},
		{"mtu_ok", func(a GreTunnelArgs) GreTunnelArgs { a.Mtu = new(1400); return a }, nil},
		{"ip_bad", func(a GreTunnelArgs) GreTunnelArgs { a.IpAddress = new("10.0.0.1"); return a }, ErrGreTunnelIPCidrInvalid},
		{"ip_v6", func(a GreTunnelArgs) GreTunnelArgs { a.IpAddress = new("fc00::1/64"); return a }, ErrGreTunnelIPCidrInvalid},
		{"ip_ok", func(a GreTunnelArgs) GreTunnelArgs { a.IpAddress = new("10.0.0.1/30"); return a }, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateGreTunnel(tc.mut(base))
			if tc.want == nil {
				if err != nil {
					t.Fatalf("unexpected: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestBuildGreTunnelCmds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		args   GreTunnelArgs
		remove bool
		want   []string
	}{
		{
			name: "minimal",
			args: GreTunnelArgs{Id: 1},
			want: []string{"interface Tunnel1"},
		},
		{
			name: "full",
			args: GreTunnelArgs{
				Id:               42,
				Description:      new("DCI to S2"),
				Mode:             new("gre"),
				Source:           new("10.0.0.1"),
				Destination:      new("10.0.0.2"),
				UnderlayVrf:      new("MGMT"),
				Tos:              new(0),
				Key:              new(12345),
				MssCeiling:       new(1300),
				PathMtuDiscovery: new(true),
				DontFragment:     new(true),
				IpAddress:        new("192.168.99.1/30"),
				Mtu:              new(1400),
				Vrf:              new("OVERLAY"),
				Shutdown:         new(false),
			},
			want: []string{
				"interface Tunnel42",
				"description DCI to S2",
				"mtu 1400",
				"vrf forwarding OVERLAY",
				"ip address 192.168.99.1/30",
				"tunnel mode gre",
				"tunnel source 10.0.0.1",
				"tunnel destination 10.0.0.2",
				"tunnel underlay vrf MGMT",
				"tunnel tos 0",
				"tunnel key 12345",
				"tunnel mss ceiling 1300",
				"tunnel path-mtu-discovery",
				"tunnel dont-fragment",
				"no shutdown",
			},
		},
		{
			name: "shutdown_true",
			args: GreTunnelArgs{Id: 5, Shutdown: new(true)},
			want: []string{"interface Tunnel5", "shutdown"},
		},
		{
			name:   "delete",
			args:   GreTunnelArgs{Id: 7},
			remove: true,
			want:   []string{"no interface Tunnel7"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := buildGreTunnelCmds(tc.args, tc.remove)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch:\ngot:\n%s\nwant:\n%s",
					strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("cmd[%d]: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestGreTunnelID(t *testing.T) {
	t.Parallel()
	if got := greTunnelID(42); got != "gre-tunnel/42" {
		t.Fatalf("got %q", got)
	}
}

func TestParseGreTunnelConfig(t *testing.T) {
	t.Parallel()
	out := strings.Join([]string{
		"interface Tunnel77",
		"   description test",
		"   mtu 1400",
		"   ip address 192.168.99.1/30",
		"   tunnel mode gre",
		"   tunnel source 10.0.0.1",
		"   tunnel destination 10.0.0.2",
		"   tunnel key 12345",
		"   tunnel path-mtu-discovery",
		"   no shutdown",
	}, "\n")

	row, ok, err := parseGreTunnelConfig(out, 77)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if !ok {
		t.Fatal("Tunnel77 not found")
	}
	if row.Description != "test" || row.Mtu != 1400 ||
		row.IpAddress != "192.168.99.1/30" || row.Mode != "gre" ||
		row.Source != "10.0.0.1" || row.Destination != "10.0.0.2" {
		t.Fatalf("fields: %+v", row)
	}
	if row.Key == nil || *row.Key != 12345 {
		t.Fatalf("key: %+v", row.Key)
	}
	if !row.PathMtuDisc {
		t.Fatalf("path-mtu-discovery should be true: %+v", row)
	}
	if row.Shutdown == nil || *row.Shutdown {
		t.Fatalf("shutdown should be false: %+v", row.Shutdown)
	}

	if _, ok, _ := parseGreTunnelConfig(out, 99); ok {
		t.Fatal("must not match different id")
	}
}
