package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ryangerardwilson/rgw-ast/internal/agents"
	"github.com/ryangerardwilson/rgw-ast/internal/config"
	"github.com/ryangerardwilson/rgw-ast/internal/enforce"
	"github.com/ryangerardwilson/rgw-ast/internal/measure"
	"github.com/ryangerardwilson/rgw-ast/internal/root"
	"github.com/ryangerardwilson/rgw-ast/internal/version"
)

// Report is a bootstrap/readiness inspection for a workspace root.
type Report struct {
	OK                bool     `json:"ok"`
	Version           string   `json:"version"`
	Commit            string   `json:"commit"`
	BuildTime         string   `json:"build_time"`
	ConfigPath        string   `json:"config_path"`
	ConfigExists      bool     `json:"config_exists"`
	Root              string   `json:"root"`
	LOC               int      `json:"loc"`
	Enforced          bool     `json:"enforced"`
	Mode              string   `json:"mode"`
	ThresholdLOC      int      `json:"threshold_loc"`
	CacheHit          bool     `json:"cache_hit"`
	GitIgnoreActive   bool     `json:"gitignore_active"`
	NestedRepos       int      `json:"nested_repos_skipped"`
	GitAvailable      bool     `json:"git_available"`
	HookReady         bool     `json:"hook_ready"`
	HookStatus        string   `json:"hook_status"` // configured|not_checked
	AgentsBlockReady  bool     `json:"agents_block_ready"`
	AgentsBlockStatus string   `json:"agents_block_status"` // current|stale|missing_block|malformed|absent_file
	GeneratorsAllow   int      `json:"generators_allow_count"`
	Notes             []string `json:"notes,omitempty"`
	Errors            []string `json:"errors,omitempty"`
}

// Run gathers diagnostics for cwd/rootOverride.
func Run(cfg config.Config, cfgPath, cwd, rootOverride string, refresh bool) Report {
	rep := Report{
		Version:           version.Version,
		Commit:            version.Commit,
		BuildTime:         version.BuildTime,
		ConfigPath:        cfgPath,
		HookReady:         false,
		HookStatus:        "not_checked",
		AgentsBlockReady:  false,
		AgentsBlockStatus: "absent_file",
		GeneratorsAllow:   len(cfg.Generators.Allow),
	}
	if _, err := os.Stat(cfgPath); err == nil {
		rep.ConfigExists = true
	} else {
		rep.Errors = append(rep.Errors, "config missing: "+cfgPath)
	}

	ws := rootOverride
	var err error
	if ws == "" {
		ws, err = root.Resolve(cwd)
		if err != nil {
			rep.Errors = append(rep.Errors, err.Error())
			rep.OK = false
			return rep
		}
	} else {
		ws, err = filepath.Abs(ws)
		if err != nil {
			rep.Errors = append(rep.Errors, err.Error())
			rep.OK = false
			return rep
		}
	}
	rep.Root = ws

	m, err := measure.CountCachedOpts(ws, cfg, measure.CountOpts{Refresh: refresh})
	if err != nil {
		rep.Errors = append(rep.Errors, "measure: "+err.Error())
	} else {
		d := enforce.Decide(cfg, m.LOC)
		rep.LOC = m.LOC
		rep.Enforced = d.Enforced
		rep.Mode = d.Mode
		rep.ThresholdLOC = d.ThresholdLOC
		rep.CacheHit = m.CacheHit
		rep.GitIgnoreActive = m.GitIgnoreActive
		rep.NestedRepos = m.NestedReposSkipped
	}

	if _, err := exec.LookPath("git"); err == nil {
		rep.GitAvailable = true
	} else {
		rep.Notes = append(rep.Notes, "git not on PATH; dirty-tree fingerprint uses mtime sampling")
	}

	// AGENTS.md managed block check against the canonical emitted block.
	agentsPath := filepath.Join(ws, "AGENTS.md")
	if data, err := os.ReadFile(agentsPath); err != nil {
		rep.AgentsBlockStatus = "absent_file"
		rep.AgentsBlockReady = false
		if rep.Enforced {
			rep.Notes = append(rep.Notes, "AGENTS.md missing; embed with: rgw-ast agents-block")
		}
	} else {
		rep.AgentsBlockStatus, rep.AgentsBlockReady = agentsBlockState(string(data))
		if rep.AgentsBlockStatus == "stale" {
			rep.Notes = append(rep.Notes, "AGENTS.md rgw-ast block differs from canonical output; refresh from: rgw-ast agents-block")
		}
	}

	// Host hooks: claim configured only for a parsed command field whose argv
	// begins with rgw-ast hook. Mere prose or unrelated JSON strings are not evidence.
	hookFound := false
	for _, p := range []string{
		filepath.Join(ws, ".codex", "hooks.json"),
		filepath.Join(ws, ".claude", "settings.json"),
		filepath.Join(ws, ".claude", "settings.local.json"),
	} {
		if data, err := os.ReadFile(p); err == nil && hasConfiguredHook(data) {
			hookFound = true
			rep.Notes = append(rep.Notes, "found rgw-ast hook command in "+p)
		}
	}
	if hookFound {
		rep.HookStatus = "configured"
		rep.HookReady = true
	} else {
		rep.HookStatus = "not_checked"
		rep.HookReady = false
		rep.Notes = append(rep.Notes, "host PreToolUse hook not observed; wire: rgw-ast hook")
	}

	if len(cfg.Generators.Allow) == 0 {
		rep.Notes = append(rep.Notes, "generators.allow empty; exec will reject all generators")
	}

	// ok means diagnostics completed without hard errors — not that all readiness is green
	rep.OK = len(rep.Errors) == 0
	return rep
}

func agentsBlockState(text string) (string, bool) {
	const begin = "<!-- rgw-ast:begin -->"
	const end = "<!-- rgw-ast:end -->"
	if strings.Count(text, begin) == 0 && strings.Count(text, end) == 0 {
		return "missing_block", false
	}
	if strings.Count(text, begin) != 1 || strings.Count(text, end) != 1 {
		return "malformed", false
	}
	start := strings.Index(text, begin)
	endStart := strings.Index(text, end)
	if start < 0 || endStart < start {
		return "malformed", false
	}
	candidate := text[start : endStart+len(end)]
	if normalizeManagedBlock(candidate) == normalizeManagedBlock(agents.Block) {
		return "current", true
	}
	return "stale", false
}

func normalizeManagedBlock(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
}

func hasConfiguredHook(data []byte) bool {
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return false
	}
	return containsHookCommand(document)
}

func containsHookCommand(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			switch strings.ToLower(key) {
			case "command", "cmd", "script":
				if isHookCommandValue(child) {
					return true
				}
			}
			if containsHookCommand(child) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsHookCommand(child) {
				return true
			}
		}
	}
	return false
}

func isHookCommandValue(value any) bool {
	switch typed := value.(type) {
	case string:
		fields := strings.Fields(typed)
		if len(fields) < 2 {
			return false
		}
		first := strings.Trim(fields[0], "\"'")
		second := strings.Trim(fields[1], "\"'")
		return filepath.Base(first) == "rgw-ast" && second == "hook"
	case []any:
		if len(typed) < 2 {
			return false
		}
		first, firstOK := typed[0].(string)
		second, secondOK := typed[1].(string)
		return firstOK && secondOK && filepath.Base(first) == "rgw-ast" && second == "hook"
	default:
		return false
	}
}

// FormatText renders a compact human report.
func FormatText(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ok: %v\n", r.OK)
	fmt.Fprintf(&b, "version: %s\n", version.String())
	fmt.Fprintf(&b, "config: %s (exists=%v)\n", r.ConfigPath, r.ConfigExists)
	fmt.Fprintf(&b, "root: %s\n", r.Root)
	fmt.Fprintf(&b, "loc: %d\nenforced: %v\nmode: %s\nthreshold_loc: %d\n", r.LOC, r.Enforced, r.Mode, r.ThresholdLOC)
	fmt.Fprintf(&b, "cache_hit: %v\ngitignore_active: %v\nnested_repos_skipped: %d\n", r.CacheHit, r.GitIgnoreActive, r.NestedRepos)
	fmt.Fprintf(&b, "git_available: %v\n", r.GitAvailable)
	fmt.Fprintf(&b, "hook_ready: %v\nhook_status: %s\n", r.HookReady, r.HookStatus)
	fmt.Fprintf(&b, "agents_block_ready: %v\nagents_block_status: %s\n", r.AgentsBlockReady, r.AgentsBlockStatus)
	fmt.Fprintf(&b, "generators_allow_count: %d\n", r.GeneratorsAllow)
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "note: %s\n", n)
	}
	for _, e := range r.Errors {
		fmt.Fprintf(&b, "error: %s\n", e)
	}
	return b.String()
}

// FormatJSON encodes the report.
func FormatJSON(r Report) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}
