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

This repo uses [OpenSpec](https://github.com/Fission-AI/OpenSpec). Accepted
capabilities live under `openspec/specs/`.

## Commands

```bash
rgw-ast help
rgw-ast version [--json]
rgw-ast config

rgw-ast [--root <path>] status [--json] [--refresh]
rgw-ast [--root <path>] measure [--json] [--refresh]
rgw-ast [--root <path>] doctor [--json] [--refresh]
rgw-ast agents-block

rgw-ast [--root <path>] map [path]
rgw-ast [--root <path>] show <symbol|path:symbol>
rgw-ast [--root <path>] search [--path <dir>] [--glob <pat>] <query>
rgw-ast search --help
rgw-ast [--root <path>] explain <path>

rgw-ast [--root <path>] hash <file> [...]
rgw-ast [--root <path>] read <file> --lines START-END [--number] [--strict-lines]
rgw-ast [--root <path>] create <file> --expect-absent (--from-file f|--stdin) [--parents]
rgw-ast [--root <path>] append <file> --expect-hash <sha> (--from-file f|--stdin)
rgw-ast [--root <path>] patch <file> --expect-hash <sha> \
  (--old/--new | --old-file/--new-file | --ops-file ops.json)

rgw-ast [--root <path>] exec [--json] -- <generator command...>
rgw-ast [--root <path>] hook
```

### Behavior notes

- Measure honors `.gitignore`, stops at nested git repos, and fingerprints
  dirty working trees (`git status --porcelain`) so nested edits invalidate cache.
- Mutations and the PreToolUse hook recompute enforcement with a fresh measure.
- When enforced, whole-file `read` is denied for any non-binary text.
- Oversized `--lines` ranges clamp to `max_read_lines` and print `next_start`
  (use `--strict-lines` to fail hard). Prefer `--number` for citations.
- Search snippets truncate to 120 characters; use `--path` / `--glob` to scope.
- Map understands Go, Bash, Markdown (headings/OpenSpec requirements), JSON keys,
  TOML sections, YAML keys, and light QML structure.
- Trusted generators (OpenSpec, etc.) must run as `rgw-ast exec -- ...` using
  patterns from global `generators.allow`.

## Local verification

```bash
go test ./...
go run ./cmd/rgw-ast help
go run ./cmd/rgw-ast version --json
go run ./cmd/rgw-ast doctor --json
```

## Install

Latest release:

```bash
curl -fsSL https://raw.githubusercontent.com/ryangerardwilson/rgw-ast/main/install.sh | bash
```

From this checkout (stamps version from git tags/commit):

```bash
./install.sh from .
```

```bash
rgw-ast version
rgw-ast version --json
```

## Release

```bash
./push_release_upgrade.sh          # next patch
./push_release_upgrade.sh 0.2.0    # explicit version
```
