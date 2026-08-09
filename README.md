# rgw-ast

Global AST-aware agent boundary CLI. One policy file for every repository:

```text
~/.config/rgw-ast/config.toml
```

(or `$XDG_CONFIG_HOME/rgw-ast/config.toml`)

No per-project config is required. When `enforcement.mode = "auto"` (default)
and measured source LOC is at least `threshold_loc` (default **5000**),
`rgw-ast status` reports `enforced: true` and whole-file reads / unhashed
patches are rejected for included source files.

## Spec-driven development

This repo uses [OpenSpec](https://github.com/Fission-AI/OpenSpec):

```bash
npm exec -- openspec list --specs
npm exec -- openspec list
npm exec -- openspec validate initial-rgw-ast-cli --strict --no-interactive
```

Accepted capabilities live under `openspec/specs/` after a change is archived.
Active work lives under `openspec/changes/`.

## Commands

```bash
rgw-ast help
rgw-ast version
rgw-ast config                 # print global config path (creates defaults)
rgw-ast status [--json]
rgw-ast measure [--json]
rgw-ast map [path]
rgw-ast show <symbol|path:symbol>
rgw-ast search <query>
rgw-ast hash <file> [...]
rgw-ast read <file> --lines START-END
rgw-ast patch <file> --expect-hash <sha256> --old <text> --new <text>
```

## Local verification

```bash
go test ./...
go run ./cmd/rgw-ast help
go run ./cmd/rgw-ast version
go run ./cmd/rgw-ast status --json
```

## Install

From this checkout:

```bash
./install.sh from .
```

Binary installs to `~/.local/bin/rgw-ast` (override with `RGW_AST_INSTALL_DIR`).

## Config defaults

On first use, `rgw-ast` writes a default global config with:

- `threshold_loc = 5000`
- `enforcement.mode = "auto"`
- include globs for common source languages
- excludes for `node_modules`, `.git`, `dist`, `build`, `.next`, `vendor`, `target`

Edit the single global file; do not add project-local policy files.

## Non-goals (v1)

MCP server, Codex/Claude hook installers, full multi-language AST rewrites,
and per-repo config overrides are out of scope for the initial release.
