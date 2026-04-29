#!/usr/bin/env bash
# Render every fenced ```mermaid``` block in Markdown files via @mermaid-js/mermaid-cli.
# A successful render proves the diagram parses; the SVG output is discarded.
#
# Requires `mmdc` (npm i -g @mermaid-js/mermaid-cli) on $PATH.

set -euo pipefail

if ! command -v mmdc >/dev/null 2>&1; then
  echo "mmdc not found; install @mermaid-js/mermaid-cli" >&2
  exit 1
fi

ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

mapfile -t MD_FILES < <(find "$ROOT" \
  -type d \( -name node_modules -o -name vendor -o -name sdk -o -name reports -o -name dist -o -name .git -o -name .worktrees \) -prune -o \
  -type f \( -name '*.md' -o -name '*.mmd' -o -name '*.markdown' \) -print)

declare -i fail=0 total=0

for md in "${MD_FILES[@]}"; do
  awk '
    /^```mermaid[[:space:]]*$/ { in_mmd=1; n++; out=FILENAME"."n".mmd"; next }
    /^```[[:space:]]*$/ && in_mmd { in_mmd=0; next }
    in_mmd { print > out }
  ' OUT="$TMP" "$md"
done

shopt -s nullglob
for diag in "$TMP"/*.mmd; do
  total+=1
  if ! mmdc --quiet -i "$diag" -o "$diag.svg" >/dev/null 2>"$diag.err"; then
    fail+=1
    echo "::error::mermaid render failed: $diag" >&2
    sed -e 's/^/    /' "$diag.err" >&2 || true
  fi
done

echo "mermaid: $total diagram(s) parsed, $fail failure(s)"
exit "$fail"
