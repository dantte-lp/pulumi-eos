#!/usr/bin/env bash
# integration-bootstrap.sh — apply the bare-minimum eAPI bootstrap config to a
# freshly-booted cEOS Lab container so integration tests can authenticate.
#
# Usage: scripts/integration-bootstrap.sh [container-name]

set -euo pipefail

CONTAINER="${1:-pulumi-eos-it-ceos}"
USERNAME="${EOS_USERNAME:-admin}"
PASSWORD="${EOS_PASSWORD:-admin}"

echo "waiting for ${CONTAINER} cEOS Cli to come up..."
for i in {1..60}; do
  if podman exec -i "${CONTAINER}" /usr/bin/Cli -p 15 -c "show version | include Software image" 2>/dev/null \
     | grep -q "Software image version"; then
    echo "  Cli ready after ${i}s"
    break
  fi
  sleep 2
done

echo "applying bootstrap config to ${CONTAINER}..."
podman exec -i "${CONTAINER}" /usr/bin/Cli -p 15 <<EOF
configure
hostname ceos1
username ${USERNAME} privilege 15 secret 0 ${PASSWORD}
no aaa root
management api http-commands
   no shutdown
   protocol http
   protocol https
   no protocol https certificate
end
write memory
EOF

echo "verifying eAPI reachable on 127.0.0.1:18080..."
for i in {1..30}; do
  if curl -fsS -u "${USERNAME}:${PASSWORD}" -X POST \
       -H "Content-Type: application/json" \
       -d '{"jsonrpc":"2.0","method":"runCmds","params":{"version":1,"cmds":["show version"],"format":"json"},"id":1}' \
       http://127.0.0.1:18080/command-api >/dev/null 2>&1; then
    echo "  eAPI reachable after ${i}s"
    exit 0
  fi
  sleep 2
done

echo "eAPI did not become reachable" >&2
exit 1
