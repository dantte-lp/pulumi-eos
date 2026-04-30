---
description: Tear down the cEOS integration test stack and remove its volumes. Use after running integration tests when the container is no longer needed.
disable-model-invocation: true
allowed-tools: Bash(podman-compose *)
---

# Tear down cEOS integration stack

Run from repo root:

```bash
podman-compose -f deployments/compose/compose.integration.yml down --volumes --remove-orphans
```

The image `localhost/ceos:4.36.0.1F` is preserved; only the container and its volumes are removed.
