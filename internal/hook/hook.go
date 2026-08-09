package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/ryangerardwilson/rgw-ast/internal/config"
	"github.com/ryangerardwilson/rgw-ast/internal/enforce"
	"github.com/ryangerardwilson/rgw-ast/internal/measure"
	"github.com/ryangerardwilson/rgw-ast/internal/root"
)

// Request is a host pre-tool-use event (Claude/Codex-like).
type Request struct {
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
	Command   string         `json:"command"`
	Name      string         `json:"name"`
}

// Decision is returned to the host.
type Decision struct {
	PermissionDecision       string        `json:"permissionDecision"`
	PermissionDecisionReason string        `json:"permissionDecisionReason,omitempty"`
	HookSpecificOutput       *hookSpecific `json:"hookSpecificOutput,omitempty"`
}

type hookSpecific struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
}

var denyTools = map[string]bool{
	"write": true, "edit": true, "create": true, "delete": true,
	"apply_patch": true, "applypatch": true, "str_replace": true,
	"search_replace": true, "searchreplace": true,
	"notebookedit": true, "notebook_edit": true,
	"createorupdatefile": true, "delete_file": true, "deletefile": true,
	"edit_file": true, "editfile": true, "write_file": true, "writefile": true,
	"multiedit": true, "multi_edit": true,
}

// Evaluate decides allow/deny for a tool call when the workspace is enforced.
func Evaluate(req Request, cfg config.Config, cwd, rootOverride string) (Decision, error) {
	ws, err := resolveRoot(cwd, rootOverride)
	if err != nil {
		return Decision{}, err
	}
	m, err := measure.CountCachedOpts(ws, cfg, measure.CountOpts{Refresh: true})
	if err != nil {
		return Decision{}, err
	}
	d := enforce.Decide(cfg, m.LOC)
	if !d.Enforced {
		return allow(), nil
	}

	name := strings.ToLower(strings.TrimSpace(req.ToolName))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(req.Name))
	}
	cmd := strings.TrimSpace(req.Command)
	if cmd == "" && req.ToolInput != nil {
		cmd = firstString(req.ToolInput, "command", "cmd", "script", "code")
	}

	// Tool name literally rgw-ast (rare MCP wrap)
	if normalizeTool(name) == "rgwast" || name == "rgw-ast" {
		return allow(), nil
	}

	if denyTools[normalizeTool(name)] {
		return deny("enforced workspace: use rgw-ast hash/patch/create/exec (not " + name + ")"), nil
	}

	if isShellish(name) {
		if isRGWAstInvocation(cmd) {
			return allow(), nil
		}
		if mutatesOutsideRGW(cmd) {
			return deny("enforced workspace: shell mutation denied; use rgw-ast patch or rgw-ast exec -- <generator>"), nil
		}
		return allow(), nil
	}
	return allow(), nil
}

// Run reads JSON request from in and writes decision JSON to out.
func Run(in io.Reader, out io.Writer, cfg config.Config, cwd, rootOverride string) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	var req Request
	if len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &req); err != nil {
			return fmt.Errorf("invalid hook json: %w", err)
		}
	}
	dec, err := Evaluate(req, cfg, cwd, rootOverride)
	if err != nil {
		dec = deny(fmt.Sprintf("rgw-ast hook error: %v", err))
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return enc.Encode(dec)
}

func resolveRoot(cwd, rootOverride string) (string, error) {
	if rootOverride != "" {
		return rootOverride, nil
	}
	return root.Resolve(cwd)
}

func allow() Decision {
	return Decision{PermissionDecision: "allow"}
}

func deny(reason string) Decision {
	return Decision{
		PermissionDecision:       "deny",
		PermissionDecisionReason: reason,
		HookSpecificOutput: &hookSpecific{
			HookEventName:            "PreToolUse",
			PermissionDecision:       "deny",
			PermissionDecisionReason: reason,
		},
	}
}

func normalizeTool(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "-", "")
	name = strings.ReplaceAll(name, "_", "")
	if i := strings.LastIndex(name, "__"); i >= 0 {
		name = name[i+2:]
	}
	return name
}

func isShellish(name string) bool {
	n := normalizeTool(name)
	switch n {
	case "bash", "shell", "terminal", "runterminalcommand", "run_terminal_command",
		"exec", "execute", "powershell", "cmd", "run":
		return true
	default:
		return strings.Contains(n, "shell") || strings.Contains(n, "bash") || strings.Contains(n, "terminal")
	}
}

// isRGWAstInvocation is true only when the leading command token is rgw-ast
// (after optional env assignments). Mentions in comments/args do not count.
func isRGWAstInvocation(cmd string) bool {
	toks := leadingTokens(cmd, 8)
	if len(toks) == 0 {
		return false
	}
	base := filepath.Base(toks[0])
	return base == "rgw-ast"
}

// leadingTokens extracts up to n shell-ish words, skipping VAR=value prefixes.
// It does not fully parse shell quoting; it is intentionally conservative.
func leadingTokens(cmd string, n int) []string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}
	// strip simple outer wrappers like "bash -lc 'rgw-ast ...'" is NOT treated
	// as rgw-ast; only direct leading binary.
	var toks []string
	i := 0
	for i < len(cmd) && len(toks) < n {
		for i < len(cmd) && unicode.IsSpace(rune(cmd[i])) {
			i++
		}
		if i >= len(cmd) {
			break
		}
		// stop at shell metacharacters that start a new command
		if strings.ContainsRune(";|&\n", rune(cmd[i])) {
			break
		}
		if cmd[i] == '#' {
			break // comment
		}
		start := i
		if cmd[i] == '\'' || cmd[i] == '"' {
			q := cmd[i]
			i++
			for i < len(cmd) && cmd[i] != q {
				if cmd[i] == '\\' && i+1 < len(cmd) {
					i += 2
					continue
				}
				i++
			}
			if i < len(cmd) {
				i++
			}
			toks = append(toks, cmd[start:i])
			continue
		}
		for i < len(cmd) && !unicode.IsSpace(rune(cmd[i])) && !strings.ContainsRune(";|&\n#", rune(cmd[i])) {
			i++
		}
		tok := cmd[start:i]
		// skip env assignments at the start of the command
		if len(toks) == 0 && strings.Contains(tok, "=") && !strings.HasPrefix(tok, "-") && !strings.Contains(tok, "/") {
			continue
		}
		toks = append(toks, tok)
	}
	// unquote simple quoted first token for basename check
	for i, t := range toks {
		if len(t) >= 2 {
			if (t[0] == '\'' && t[len(t)-1] == '\'') || (t[0] == '"' && t[len(t)-1] == '"') {
				toks[i] = t[1 : len(t)-1]
			}
		}
	}
	return toks
}

func mutatesOutsideRGW(cmd string) bool {
	if cmd == "" {
		return false
	}
	if isRGWAstInvocation(cmd) {
		return false
	}
	low := strings.ToLower(cmd)
	patterns := []string{
		"sed -i", "perl -i", "ruby -i",
		"rm ", "rm\t", "unlink ", "truncate ",
		"mv ", "cp ",
		"tee ",
		"apply_patch", "git apply", "git checkout --",
		"chmod ", "chown ",
	}
	for _, p := range patterns {
		if strings.Contains(low, p) {
			return true
		}
	}
	// redirects are mutations; bare echo/printf without redirect is not
	if strings.Contains(cmd, ">") || strings.Contains(cmd, ">>") {
		return true
	}
	return false
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}
