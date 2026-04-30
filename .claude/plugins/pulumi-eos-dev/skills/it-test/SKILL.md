---
description: Run integration tests against the running cEOS container. Use after `/pulumi-eos-dev:it-up` to verify a code change end-to-end.
disable-model-invocation: true
allowed-tools: Bash(podman-compose *)
---

# Run integration tests against cEOS

Run from repo root:

```bash
podman-compose -f deployments/compose/compose.dev.yml exec -T dev \
  bash -c 'cd /app && EOS_HOST=host.containers.internal go test -tags integration -count=1 -v ./test/integration/...'
```

Tests run inside the dev container. `host.containers.internal` resolves to the podman bridge gateway, where cEOS publishes its eAPI ports.

If the dev container is not yet started, start it with `make up` first.
