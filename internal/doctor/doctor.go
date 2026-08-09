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
	OK                 bool     `json:"ok"`
	Version            string   `json:"version"`
	Commit             string   `json:"commit"`
	BuildTime          string   `json:"build_time"`
	ConfigPath         string   `json:"config_path"`
	ConfigExists       bool     `json:"config_exists"`
	Root               string   `json:"root"`
	LOC                int      `json:"loc"`
	Enforced           bool     `json:"enforced"`
	Mode               string   `json:"mode"`
	ThresholdLOC       int      `json:"threshold_loc"`
	CacheHit           bool     `json:"cache_hit"`
	GitIgnoreActive    bool     `json:"gitignore_active"`
	NestedRepos         int      `json:"nested_repos_skipped"`
	GitAvailable       bool     `json:"git_available"`
	HookReady          bool     `json:"hook_ready"`
	HookStatus         string   `json:"hook_status"` // ready|not_checked|missing
	AgentsBlockReady   bool     `json:"agents_block_ready"`
	AgentsBlockStatus  string   `json:"agents_block_status"` // current|missing|missing_block|stale|absent_file
	GeneratorsAllow    int      `json:"generators_allow_count"`
	Notes              []string `json:"notes,omitempty"`
	Errors             []string `json:"errors,omitempty"`
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

	// AGENTS.md managed block check
	agentsPath := filepath.Join(ws, "AGENTS.md")
	if data, err := os.ReadFile(agentsPath); err != nil {
		rep.AgentsBlockStatus = "absent_file"
		rep.AgentsBlockReady = false
		if rep.Enforced {
			rep.Notes = append(rep.Notes, "AGENTS.md missing; embed with: rgw-ast agents-block")
		}
	} else {
		text := string(data)
		hasBegin := strings.Contains(text, "<!-- rgw-ast:begin -->")
		hasEnd := strings.Contains(text, "<!-- rgw-ast:end -->")
		switch {
		case hasBegin && hasEnd:
			// compare core markers from canonical block
			if strings.Contains(text, "rgw-ast status --json") && strings.Contains(text, "rgw-ast exec") {
				rep.AgentsBlockStatus = "current"
				rep.AgentsBlockReady = true
			} else {
				rep.AgentsBlockStatus = "stale"
				rep.AgentsBlockReady = false
				rep.Notes = append(rep.Notes, "AGENTS.md has rgw-ast markers but may be stale; refresh from: rgw-ast agents-block")
			}
		case hasBegin || hasEnd:
			rep.AgentsBlockStatus = "malformed"
			rep.AgentsBlockReady = false
		default:
			rep.AgentsBlockStatus = "missing_block"
			rep.AgentsBlockReady = false
		}
	}
	_ = agents.Block // ensure package linked; canonical source of truth for agents-block command

	// Host hooks: best-effort observation only — never claim ready without evidence.
	hookFound := false
	for _, p := range []string{
		filepath.Join(ws, ".codex", "hooks.json"),
		filepath.Join(ws, ".claude", "settings.json"),
		filepath.Join(ws, ".claude", "settings.local.json"),
	} {
		if data, err := os.ReadFile(p); err == nil && strings.Contains(string(data), "rgw-ast") {
			hookFound = true
			rep.Notes = append(rep.Notes, "found rgw-ast mention in "+p)
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
