package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

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
	// Alternate host fields
	Name string `json:"name"`
}

// Decision is returned to the host.
type Decision struct {
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	// Claude-compatible nested shape
	HookSpecificOutput *hookSpecific `json:"hookSpecificOutput,omitempty"`
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
	// Always refresh measure for enforcement decisions (hook boundary).
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

	if isRGWAst(name, cmd, req.ToolInput) {
		return allow(), nil
	}
	if denyTools[normalizeTool(name)] {
		return deny("enforced workspace: use rgw-ast hash/patch/create/exec (not " + name + ")"), nil
	}
	if isShellish(name) {
		if isRGWAstExec(cmd) {
			return allow(), nil
		}
		if mutatesOutsideRGW(cmd) {
			return deny("enforced workspace: shell mutation denied; use rgw-ast patch or rgw-ast exec -- <generator>"), nil
		}
	}
	return allow(), nil
}

func isRGWAstExec(cmd string) bool {
	low := strings.ToLower(strings.TrimSpace(cmd))
	return strings.HasPrefix(low, "rgw-ast exec") || strings.Contains(low, "/rgw-ast exec")
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
		// fail open for measure errors? prefer fail closed for safety when we can't decide
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
	// strip server prefixes like mcp__x__Write
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

func isRGWAst(name, cmd string, input map[string]any) bool {
	if strings.Contains(strings.ToLower(name), "rgw-ast") || strings.Contains(strings.ToLower(name), "rgwast") {
		return true
	}
	blob := strings.ToLower(cmd)
	if input != nil {
		b, _ := json.Marshal(input)
		blob += " " + strings.ToLower(string(b))
	}
	return strings.Contains(blob, "rgw-ast")
}

func mutatesOutsideRGW(cmd string) bool {
	if cmd == "" {
		return false
	}
	c := strings.TrimSpace(cmd)
	low := strings.ToLower(c)
	if strings.HasPrefix(low, "rgw-ast") || strings.Contains(low, "/rgw-ast ") {
		return false
	}
	// common mutation patterns
	patterns := []string{
		"sed -i", "perl -i", "ruby -i",
		"rm ", "rm\t", "unlink ", "truncate ",
		"mv ", "cp ", "install ",
		"tee ", ">", ">>",
		"apply_patch", "git apply", "git checkout --",
		"chmod ", "chown ",
	}
	for _, p := range patterns {
		if strings.Contains(low, p) {
			// allow rg redirects that only pipe to rgw-ast? rare
			if p == ">" || p == ">>" {
				if strings.Contains(low, "rgw-ast") {
					continue
				}
			}
			return true
		}
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
