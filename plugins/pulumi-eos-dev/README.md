# pulumi-eos-dev (project plugin)

Project-local Claude Code plugin bundling skills used by `pulumi-eos`
maintainers and contributors.

## Skills

| Skill | Type | Purpose |
|---|---|---|
| `/pulumi-eos-dev:it-up` | manual | Bring up cEOS Lab 4.36.0.1F + apply bootstrap. |
| `/pulumi-eos-dev:it-down` | manual | Tear down cEOS Lab. |
| `/pulumi-eos-dev:it-test` | manual | Run `go test -tags integration` inside dev container. |
| `/pulumi-eos-dev:gates` | manual | Run every quality gate (Go + docs + spell + yaml). |
| `/pulumi-eos-dev:status` | auto | Project snapshot: commits, sprints, container health, CI. |
| `/pulumi-eos-dev:new-l2-resource <Name>` | manual | Scaffold a new `eos:l2:*` resource against the `vlan.go` pattern. |
| `/pulumi-eos-dev:arista-fact <topic>` | auto | Mandatory ground-truth check via `arista-mcp` before claiming any Arista fact. |

## Install

The plugin lives under `.claude/plugins/pulumi-eos-dev/` in this repo. Two
ways to enable it:

### Per-session (preferred for development)

```bash
claude --plugin-dir .claude/plugins/pulumi-eos-dev
```

### Per-user (persistent)

Add the plugin folder to your enabled plugins via the `/plugin` command in
Claude Code, or import the marketplace defined under
`.claude-plugin/marketplace.json` (when present in the repo).

## Layout

```text
.claude/plugins/pulumi-eos-dev/
├── .claude-plugin/
│   └── plugin.json
├── skills/
│   ├── arista-fact/SKILL.md
│   ├── gates/SKILL.md
│   ├── it-down/SKILL.md
│   ├── it-test/SKILL.md
│   ├── it-up/SKILL.md
│   ├── new-l2-resource/SKILL.md
│   └── status/SKILL.md
└── README.md
```

## Standards

| Convention | Source |
|---|---|
| `disable-model-invocation: true` for state-mutating skills | Claude Code Skills docs — control who invokes a skill. |
| `allowed-tools` listed for every skill that runs commands | Claude Code Skills docs — pre-approve tools. |
| Skill body describes _what to run_ declaratively | Project rule (`docs/09-go-style.md` style). |
| Plugin metadata (`name`, `description`, `version`, `license`) | Claude Code Plugins docs — `plugin.json` schema. |
