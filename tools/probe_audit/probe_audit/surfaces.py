"""Per-resource CLI surface specs.

Each entry mirrors the optional / required fields rendered by the
matching Go resource in ``internal/resources/l3/``. The probe-audit
harness commits each line individually under the resource's fixture
context. A line that succeeds + persists into running-config is
correct; a line that fails or silently no-ops on cEOS surfaces a
keyword bug.

Adding a resource:

1. Define a `Surface` with `name`, `fixture` (lines that establish
   the parent context), `cleanup` (lines that revert), `probes`
   (one CLI line per Args field).
2. Reference the EOS User Manual section that justifies each line
   — keeps the audit traceable.
"""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True)
class Surface:
    """One resource's audit surface."""

    name: str
    fixture: list[str]
    cleanup: list[str]
    probes: list[str]


SURFACES: list[Surface] = [
    Surface(
        name="loopback",
        fixture=["interface Loopback99"],
        cleanup=["no interface Loopback99"],
        probes=[
            "description audit",
            "ip address 192.0.2.1/32",
            "ipv6 address 2001:db8::1/128",
            "shutdown",
            "no shutdown",
        ],
    ),
    Surface(
        name="vrf",
        fixture=["vrf instance AUDIT_VRF"],
        cleanup=["no vrf instance AUDIT_VRF"],
        probes=[
            "rd 65000:1",
            "description audit",
            "ip routing vrf AUDIT_VRF",
            "ipv6 unicast-routing vrf AUDIT_VRF",
        ],
    ),
    Surface(
        name="bfd",
        fixture=["router bfd"],
        cleanup=["no router bfd"],
        probes=[
            "interval 300 min-rx 300 multiplier 3 default",
            "slow-timer 2000",
            "no shutdown",
        ],
    ),
    Surface(
        name="subinterface",
        # Subinterface lives on a routed parent; cEOS Lab needs the
        # parent to be `no switchport` first.
        fixture=[
            "interface Ethernet1",
            "no switchport",
            "exit",
            "interface Ethernet1.100",
        ],
        cleanup=[
            "no interface Ethernet1.100",
            "interface Ethernet1",
            "switchport",
        ],
        probes=[
            "encapsulation dot1q vlan 100",
            "description audit",
            "ip address 10.99.0.1/30",
            # `mtu` on a subinterface is rejected on cEOSLab as
            # "Unavailable command (not supported on this hardware
            # platform)". Production EOS accepts it. Resource takes
            # the input but the line is platform-conditional.
            "no shutdown",
        ],
    ),
    Surface(
        name="static_route",
        fixture=["ip routing"],
        cleanup=[
            "no ip route 192.0.2.0/24 Null0",
            "no ip route 192.0.2.0/24 10.0.0.1",
            "no ip routing",
        ],
        probes=[
            "ip route 192.0.2.0/24 Null0",
            "ip route 192.0.2.0/24 10.0.0.1",
            "ip route 192.0.2.0/24 10.0.0.1 1 tag 7",
            "ip route 192.0.2.0/24 10.0.0.1 name AUDIT",
        ],
    ),
    Surface(
        name="route_map",
        # The probe also creates the prerequisite community-list /
        # prefix-list / as-path-list / extcommunity-list so `match`
        # and `set` lines that take a referenced name pass parser
        # validation. Without these, EOS rejects the line with
        # "Invalid input (at token 2: '<NAME>')".
        fixture=[
            "ip community-list AUDIT_CL permit 65000:1",
            "ip extcommunity-list AUDIT_EX permit rt 65000:1",
            "ip as-path access-list AUDIT_AS permit ^65000$",
            "ip prefix-list AUDIT_PL seq 10 permit 10.0.0.0/8",
            "route-map AUDIT_RM permit 10",
        ],
        cleanup=[
            "no route-map AUDIT_RM",
            "no ip community-list AUDIT_CL",
            "no ip extcommunity-list AUDIT_EX",
            "no ip as-path access-list AUDIT_AS",
            "no ip prefix-list AUDIT_PL",
        ],
        probes=[
            "match as-path AUDIT_AS",
            "match community AUDIT_CL",
            "match extcommunity AUDIT_EX",
            "match interface Ethernet1",
            "match ip address prefix-list AUDIT_PL",
            # `match ipv6 address prefix-list <NAME>` requires a
            # pre-existing ipv6 prefix-list; cEOSLab rejects the
            # `ipv6 prefix-list <NAME> seq N permit ::/0` create
            # form so the audit cannot reliably exercise this line.
            # Documented as deferred coverage rather than retried.
            "match local-preference 100",
            "match metric 50",
            "match route-type internal",
            "match source-protocol bgp",
            "match tag 42",
            "set as-path prepend 65000 65001",
            "set community 65000:100 additive",
            "set distance 110",
            "set extcommunity rt 65000:1 additive",
            "set ip next-hop 10.0.0.1",
            "set ipv6 next-hop 2001:db8::1",  # link-local prohibited
            "set local-preference 200",
            "set metric 50",
            "set metric-type type-1",
            "set origin igp",
            "set tag 42",
            "set weight 1000",
            "continue 20",
            "description audit",
        ],
    ),
    Surface(
        name="prefix_list",
        fixture=[],
        cleanup=["no ip prefix-list AUDIT_PL"],
        probes=[
            "ip prefix-list AUDIT_PL seq 10 permit 10.0.0.0/8 ge 16 le 24",
            "ip prefix-list AUDIT_PL seq 20 deny 192.168.0.0/16 ge 24",
            "ip prefix-list AUDIT_PL seq 30 permit 0.0.0.0/0",
        ],
    ),
    Surface(
        name="community_list",
        fixture=[],
        cleanup=[
            "no ip community-list AUDIT_CL",
            "no ip community-list AUDIT_RX",
        ],
        probes=[
            "ip community-list AUDIT_CL permit internet",
            "ip community-list AUDIT_CL permit 65000:1",
            "ip community-list AUDIT_CL permit 4294967040",
            "ip community-list AUDIT_CL deny no-export",
            "ip community-list regexp AUDIT_RX permit ^65000:.*",
        ],
    ),
    Surface(
        name="ext_community_list",
        fixture=[],
        cleanup=[
            "no ip extcommunity-list AUDIT_EX",
            "no ip extcommunity-list AUDIT_EXR",
        ],
        probes=[
            "ip extcommunity-list AUDIT_EX permit rt 65000:1",
            "ip extcommunity-list AUDIT_EX permit soo 65000:2",
            "ip extcommunity-list regexp AUDIT_EXR permit ^RT:.*",
        ],
    ),
    Surface(
        name="as_path_access_list",
        fixture=[],
        cleanup=["no ip as-path access-list AUDIT_AS"],
        probes=[
            "ip as-path access-list AUDIT_AS permit ^65000$",
            "ip as-path access-list AUDIT_AS deny _65003_",
        ],
    ),
    Surface(
        name="rpki",
        fixture=["router bgp 65000", "rpki cache AUDIT_C"],
        cleanup=[
            "router bgp 65000",
            "no rpki cache AUDIT_C",
            "no router bgp 65000",
        ],
        probes=[
            "host 192.0.2.1",
            "host 192.0.2.1 vrf MGMT port 3323",
            "preference 4",
            "refresh-interval 30",
            "retry-interval 10",
            "expire-interval 600",
            "local-interface Management0",
            "transport tcp",
            # `transport ssh` is rejected on cEOSLab 4.36 ("Invalid
            # input"); production EOS accepts it once an SSH server
            # / known-hosts entry is wired up. The resource accepts
            # both at the input layer (see vrrp.go:validVrrpTransport).
            # Documented as a cEOSLab quirk; not exercised live.
        ],
    ),
    Surface(
        name="router_ospf",
        fixture=["ip routing", "router ospf 1"],
        cleanup=["no router ospf 1", "no ip routing"],
        # Only the lines that are NOT exercised by the existing
        # integration test — the audit complements coverage rather
        # than duplicating it. See test/integration/router_ospf_test.go.
        probes=[
            "auto-cost reference-bandwidth 100000",
            "distance ospf intra-area 90",
            "distance ospf inter-area 95",
            "distance ospf external 110",
            "default-information originate metric 100 metric-type 2",
            "default-information originate route-map AUDIT_RM",
            "summary-address 10.0.0.0/16",
            "log-adjacency-changes detail",
            "graceful-restart-helper",
            "timers spf delay initial 50 200 5000",
            "timers out-delay 100",
            "timers pacing flood 33",
            "area 0 default-cost 10",
            "area 0 range 10.0.0.0/16",
            "area 0.0.0.4 nssa default-information-originate metric 100 metric-type 2 nssa-only",
        ],
    ),
    # eos:l3:Rcf surface intentionally OMITTED from probe-audit:
    # cEOSLab 4.36.0.1F does not register the `code unit` / `pull
    # unit` keywords (RCF is a hardware-platform feature). The
    # existing integration test exercises the SourceFile delivery
    # path against cEOSLab via the rich-command stub; full surface
    # coverage requires a physical Arista platform and is tracked
    # under STATUS Open commitments.
    Surface(
        name="gre_tunnel",
        fixture=["interface Tunnel88"],
        cleanup=["no interface Tunnel88"],
        probes=[
            "tunnel mode gre",
            # mpls-gre / mpls-over-gre rejected on cEOSLab (TOI 18464
            # — pseudowire mode requires MPLS fabric context).
            # Deferred to v1 `eos:l3:GreTunnelMpls`.
            "tunnel source 10.0.0.1",
            "tunnel destination 10.0.0.2",
            "tunnel underlay vrf MGMT",
            "tunnel tos 0",
            "tunnel key 12345",
            "tunnel mss ceiling 1300",
            "tunnel path-mtu-discovery",
            "tunnel dont-fragment",  # known cEOSLab quirk
            "ip address 192.168.99.1/30",
            "mtu 1400",
            "description audit",
            "shutdown",
            "no shutdown",
        ],
    ),
]


def get(name: str) -> Surface:
    """Look up a surface by name."""
    for s in SURFACES:
        if s.name == name:
            return s
    raise KeyError(name)
