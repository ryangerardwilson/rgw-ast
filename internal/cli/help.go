package cli

import "io"

const helpText = `rgw-ast — global AST-aware agent boundary

Usage:
  rgw-ast [--root <path>] help
  rgw-ast [--root <path>] version
  rgw-ast config
  rgw-ast [--root <path>] status [--json]
  rgw-ast [--root <path>] measure [--json]
  rgw-ast [--root <path>] map [path]
  rgw-ast [--root <path>] show <symbol|path:symbol>
  rgw-ast [--root <path>] search <query>
  rgw-ast [--root <path>] hash <file> [<file>...]
  rgw-ast [--root <path>] read <file> --lines <start>-<end>
  rgw-ast [--root <path>] patch <file> --expect-hash <sha256> --old <text> --new <text>
  rgw-ast [--root <path>] hook   # PreToolUse JSON on stdin → decision JSON

Policy is global only: ~/.config/rgw-ast/config.toml (or $XDG_CONFIG_HOME).
No per-project config file is required. LOC is cached (~60s) under the XDG cache.

When enforcement is active (mode=auto and LOC >= threshold, default 5000):
  - prefer map/show/search over whole-file reads
  - read requires --lines for any non-binary text file
  - patch requires a fresh --expect-hash
  - agent hosts should pipe PreToolUse events to: rgw-ast hook

Exit codes: 0 success, 1 runtime error, 2 usage error
(hook always exits 0 after writing a decision)
`

func WriteHelp(w io.Writer) {
	_, _ = io.WriteString(w, helpText)
}
