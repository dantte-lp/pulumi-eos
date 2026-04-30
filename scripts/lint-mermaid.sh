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

# Puppeteer launches headless Chromium. Running as root inside containers and
# the GitHub Actions runners requires --no-sandbox. We also pin the Chrome
# binary explicitly when one is found in the puppeteer cache, since mmdc's
# bundled puppeteer-core occasionally drifts ahead of the installed Chrome
# build and refuses to launch the older bundled binary.
PUPPETEER_CFG="$TMP/puppeteer.json"
CHROME_BIN=""
for path in \
    "$(find "${HOME:-/root}/.cache/puppeteer/chrome" -name chrome -executable -path '*/chrome-linux64/*' 2>/dev/null | sort | tail -1)" \
    "$(command -v chromium-browser 2>/dev/null || true)" \
    "$(command -v google-chrome 2>/dev/null || true)" \
    "$(command -v chromium 2>/dev/null || true)"; do
  if [ -n "$path" ] && [ -x "$path" ]; then
    CHROME_BIN="$path"; break
  fi
done
if [ -n "$CHROME_BIN" ]; then
  printf '{"executablePath":"%s","args":["--no-sandbox","--disable-setuid-sandbox"]}\n' \
    "$CHROME_BIN" > "$PUPPETEER_CFG"
else
  printf '{"args":["--no-sandbox","--disable-setuid-sandbox"]}\n' > "$PUPPETEER_CFG"
fi

mapfile -t MD_FILES < <(find "$ROOT" \
  -type d \( -name node_modules -o -name vendor -o -name sdk -o -name reports -o -name dist -o -name .git -o -name .worktrees \) -prune -o \
  -type f \( -name '*.md' -o -name '*.mmd' -o -name '*.markdown' \) -print)

declare -i fail=0 total=0

for md in "${MD_FILES[@]}"; do
  base="$(basename "$md")"
  awk -v out_dir="$TMP" -v stem="$base" '
    /^```mermaid[[:space:]]*$/ { in_mmd=1; n++; out=out_dir "/" stem "." n ".mmd"; next }
    /^```[[:space:]]*$/ && in_mmd { in_mmd=0; next }
    in_mmd { print > out }
  ' "$md"
done

shopt -s nullglob
for diag in "$TMP"/*.mmd; do
  total+=1
  if ! mmdc --quiet --puppeteerConfigFile "$PUPPETEER_CFG" -i "$diag" -o "$diag.svg" >/dev/null 2>"$diag.err"; then
    fail+=1
    echo "::error::mermaid render failed: $diag" >&2
    sed -e 's/^/    /' "$diag.err" >&2 || true
  fi
done

echo "mermaid: $total diagram(s) parsed, $fail failure(s)"
exit "$fail"
