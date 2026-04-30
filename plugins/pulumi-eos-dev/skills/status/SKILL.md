---
description: Show the project status snapshot — current sprint, recent commits, cEOS container health, dev container health. Use to orient at the start of a session.
allowed-tools: Bash(git *) Bash(podman *)
---

# Project status snapshot

Run from repo root:

```bash
echo '== last 8 commits =='
git log --oneline | head -8

echo '== sprint state =='
awk '/^## Sprint progress$/,/^## Quality gates/{if(/^\| S/) print}' docs/STATUS.md

echo '== containers =='
podman ps --filter name=pulumi-eos --format '{{.Names}}\t{{.Status}}'

echo '== gh CI =='
gh run list --branch main --limit 3 --json status,conclusion,name 2>/dev/null
```

The output gives you: where the codebase is in the waterfall plan, whether cEOS / dev containers are running, whether CI is green for the latest pushed commit.
