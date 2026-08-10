package cli

import "io"

const helpText = `rgw-ast — global AST-aware agent boundary

Usage:
  rgw-ast [--root <path>] <command> [args]

Commands:
  help | version [--json] | config
  status [--json] [--refresh]
  measure [--json] [--refresh]
  doctor [--json] [--refresh]
  agents-block
  map [path]
  show <symbol|path:symbol>
  search [--path <dir>] [--glob <pat>] <query>
  search --help
  explain <path>
  hash <file>...
  read <file> --lines <a>-<b> [--number] [--strict-lines]
  create <file> --expect-absent (--from-file <f>|--stdin) [--parents]
  append <file> --expect-hash <sha> (--from-file <f>|--stdin)
  patch <file> --expect-hash <sha> (--old/--new | --old-file/--new-file | --ops-file <json>)
  delete <file> --expect-hash <sha> [--prune-empty]
  exec [--json] -- <generator command...>
  hook

Policy: ~/.config/rgw-ast/config.toml only.
Measure honors .gitignore, stops at nested git repos, fingerprints dirty trees.
LOC cached ~60s; mutations/hook refresh enforcement. generators.allow lists exec.

When enforced:
  map/show/search + read --lines; create/append/patch/delete for mutations;
  generators only via: rgw-ast exec -- <cmd>
  Hosts: pipe PreToolUse to rgw-ast hook.

Exit: 0 ok, 1 error, 2 usage (hook always 0 after decision JSON)
`

const searchHelp = `rgw-ast search — literal substring search

Usage:
  rgw-ast search [--path <dir>] [--glob <pattern>] <query>
  rgw-ast search --help

Flags:
  --path   limit walk to this workspace-relative directory or file
  --glob   match basenames or paths (e.g. *.sh)
`

func WriteHelp(w io.Writer) {
	_, _ = io.WriteString(w, helpText)
}

func WriteSearchHelp(w io.Writer) {
	_, _ = io.WriteString(w, searchHelp)
}
