package cli

import "io"

const helpText = `rgw-ast — global AST-aware agent boundary

Usage:
  rgw-ast help
  rgw-ast version
  rgw-ast config
  rgw-ast status [--json]
  rgw-ast measure [--json]
  rgw-ast map [path]
  rgw-ast show <symbol|path:symbol>
  rgw-ast search <query>
  rgw-ast hash <file> [<file>...]
  rgw-ast read <file> --lines <start>-<end>
  rgw-ast patch <file> --expect-hash <sha256> --old <text> --new <text>

Policy is global only: ~/.config/rgw-ast/config.toml (or $XDG_CONFIG_HOME).
No per-project config file is required.

When enforcement is active (mode=auto and LOC >= threshold, default 5000):
  - prefer map/show/search over whole-file reads
  - read requires --lines
  - patch requires a fresh --expect-hash

Exit codes: 0 success, 1 runtime error, 2 usage error
`

func WriteHelp(w io.Writer) {
	_, _ = io.WriteString(w, helpText)
}
