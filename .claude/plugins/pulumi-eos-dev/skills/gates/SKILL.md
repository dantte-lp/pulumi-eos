---
description: Run every quality gate the project enforces — Go build / test / golangci-lint inside the dev container, plus markdownlint, mermaid render, cspell, yamllint on the host. Use before committing.
disable-model-invocation: true
allowed-tools: Bash(podman-compose *) Bash(markdownlint-cli2 *) Bash(bash scripts/lint-mermaid.sh) Bash(yamllint *) Bash(npx *)
---

# Run all quality gates

Run from repo root:

```bash
echo '== build + lint + test (dev container) =='
podman-compose -f deployments/compose/compose.dev.yml exec -T dev \
  bash -c 'cd /app && go build -buildvcs=false ./... && golangci-lint run ./... && go test -buildvcs=false -race -count=1 ./...'

echo '== markdownlint =='
markdownlint-cli2 "**/*.md" "#node_modules" "#vendor" "#sdk" "#reports" "#dist"

echo '== mermaid render =='
bash scripts/lint-mermaid.sh

echo '== yamllint =='
yamllint -c .yamllint.yaml .

echo '== cspell =='
npx --quiet cspell --no-progress --no-summary --config .cspell.json "**/*.md" "**/*.go"
```

Every gate must exit clean before pushing. The Go gate covers ~70 linters in allowlist mode; severity-tier `error` findings block CI.
