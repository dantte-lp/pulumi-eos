# Resource catalog

> Initial v0.x resource set. Per-resource design pages under `docs/resource-catalog/<area>/<name>.md` are generated from the Pulumi schema in S3.

## `eos:device`

| Resource | Transport | Sprint |
|---|---|---|
| `Device` | eAPI / gNMI Get | S4 |
| `Configlet` | eAPI · config session | S4 |
| `RawCli` | eAPI · config session | S5 |
| `OsImage` | gNOI · `OS.Install` | S9 |
| `Reboot` | gNOI · `System.Reboot` | S9 |
| `Certificate` | gNOI · `Cert.Rotate` | S9 |

## `eos:l2`

| Resource | Transport | Sprint |
|---|---|---|
| `Vlan` | eAPI · config session | S5 |
| `VlanInterface` | eAPI · config session | S5 |
| `PortChannel` | eAPI · config session | S5 |
| `Mlag` | eAPI · config session | S5 |
| `VxlanInterface` | eAPI · config session | S5 |

## `eos:l3`

| Resource | Transport | Sprint |
|---|---|---|
| `Interface` | eAPI · config session | S6 |
| `Vrf` | eAPI · config session | S6 |
| `RouterBgp` | eAPI · config session | S6 |
| `RouterOspf` | eAPI · config session | S6 |

## `eos:security`

| Resource | Transport | Sprint |
|---|---|---|
| `Acl` | eAPI · config session | S7 |
| `RouteMap` | eAPI · config session | S7 |
| `UserAccount` | eAPI · config session | S7 |
| `SslProfile` | eAPI · config session | S7 |

## `eos:management`

| Resource | Transport | Sprint |
|---|---|---|
| `NtpServer` | eAPI | S7 |
| `DnsServer` | eAPI | S7 |
| `Logging` | eAPI | S7 |
| `AaaServer` | eAPI | S7 |

## `eos:cvp`

| Resource | Transport | Sprint |
|---|---|---|
| `Workspace` | CVP gRPC · `workspace.v1` | S8 |
| `Studio` | CVP gRPC · `studio.v1` | S8 |
| `Configlet` | CVP gRPC · `configlet.v1` | S8 |
| `ChangeControl` | CVP gRPC · `changecontrol.v1` | S8 |
| `Tag` | CVP gRPC · `tag.v2` | S8 |
| `Device` | CVP gRPC · `inventory.v1` | S8 |
| `Inventory` | CVP gRPC · `inventory.v1` | S8 |
| `ServiceAccount` | CVP gRPC · `serviceaccount.v1` | S8 |
