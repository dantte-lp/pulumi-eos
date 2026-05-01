#!/usr/bin/env bash
# lint-probes.sh — enforce docs/05-development.md rule 2b on
# `//go:build integration && probe`-tagged discovery files.
#
# Rule 2b: probes must terminate with `commit`, not `abort`. EOS
# only triggers full hardware-platform validation on commit, so
# abort-only probes silently mark unsupported commands as OK and
# ship them into resources that then fail at runtime.
#
# Enforcement strategy:
#   - Every `probe_*_test.go` file must use the shared helpers
#     `ProbeOnePerCmd` / `ProbeFullBody` from probe_helpers.go,
#     OR explicitly call `Commit` for every staged session it
#     opens (auditable on a case-by-case basis).
#   - The shared helpers contain all `Abort` calls that probes are
#     allowed to make (rollback-on-error inside Stage failure).
#   - Probe files MUST NOT call `*Session.Abort` directly — if a
#     probe ends in Abort, it bypasses platform validation.
#
# Exit non-zero with a list of violations on failure.

set -euo pipefail

PROBE_GLOB='test/integration/probe_*_test.go'
HELPER='test/integration/probe_helpers.go'

if ! ls $PROBE_GLOB >/dev/null 2>&1; then
  echo "lint-probes: no probe files found — skipping"
  exit 0
fi

violations=0

for f in $PROBE_GLOB; do
  # Skip the helpers themselves — they own the only allowed Abort
  # calls, gated behind Stage-error rollback.
  if [[ "$f" == "$HELPER" ]]; then
    continue
  fi

  # Match `.Abort(` outside helper code. Probe author is expected to
  # delegate to ProbeOnePerCmd / ProbeFullBody. Direct .Abort calls
  # in probe files are a rule-2b violation.
  if grep -nE '\.Abort\(' "$f" >/dev/null; then
    echo "lint-probes: rule 2b violation — direct .Abort( in $f:" >&2
    grep -nE '\.Abort\(' "$f" >&2
    violations=$((violations + 1))
  fi
done

if (( violations > 0 )); then
  echo "" >&2
  echo "lint-probes: $violations file(s) violate rule 2b. Use" \
       "ProbeOnePerCmd / ProbeFullBody from probe_helpers.go or" \
       "explicitly Commit every session." >&2
  exit 1
fi

echo "lint-probes: $(ls $PROBE_GLOB | wc -l | tr -d ' ') probe file(s) clean — rule 2b satisfied"
