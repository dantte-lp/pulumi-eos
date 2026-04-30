---
description: Bring up the cEOS integration test stack (Arista cEOS Lab 4.36.0.1F) and apply the eAPI bootstrap config. Use before running integration tests.
disable-model-invocation: true
allowed-tools: Bash(podman *) Bash(podman-compose *) Bash(bash scripts/integration-bootstrap.sh *)
---

# Bring up cEOS integration stack

Run from repo root:

```bash
podman ps --filter name=ceos --format '{{.Names}} {{.Status}}'
podman-compose -f deployments/compose/compose.integration.yml up -d
until podman exec -i pulumi-eos-it-ceos /usr/bin/Cli -p 15 -c "show version | include Software image" 2>/dev/null | grep -q "Software image version"; do sleep 2; done
bash scripts/integration-bootstrap.sh pulumi-eos-it-ceos
```

The bootstrap script applies the admin user and enables eAPI HTTP/HTTPS. eAPI is reachable on `http://127.0.0.1:18080/command-api` once it returns, or `http://host.containers.internal:18080/command-api` from inside the dev container.

If `localhost/ceos:4.36.0.1F` image is missing on the host, re-import via:

```bash
xz -dc /opt/software/cEOS64-lab-4.36.0.1F.tar | podman import - localhost/ceos:4.36.0.1F
```
