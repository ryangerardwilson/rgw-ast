package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ryangerardwilson/rgw-ast/internal/config"
	"github.com/ryangerardwilson/rgw-ast/internal/enforce"
	"github.com/ryangerardwilson/rgw-ast/internal/measure"
	"github.com/ryangerardwilson/rgw-ast/internal/root"
	"github.com/ryangerardwilson/rgw-ast/internal/version"
)

// Report is a bootstrap/readiness inspection for a workspace root.
type Report struct {
	OK              bool   `json:"ok"`
	Version         string `json:"version"`
	Commit          string `json:"commit"`
	BuildTime       string `json:"build_time"`
	ConfigPath      string `json:"config_path"`
	ConfigExists    bool   `json:"config_exists"`
	Root            string `json:"root"`
	LOC             int    `json:"loc"`
	Enforced        bool   `json:"enforced"`
	Mode            string `json:"mode"`
	ThresholdLOC    int    `json:"threshold_loc"`
	CacheHit        bool   `json:"cache_hit"`
	GitIgnoreActive bool   `json:"gitignore_active"`
	NestedRepos      int    `json:"nested_repos_skipped"`
	GitAvailable    bool   `json:"git_available"`
	HookReady       bool   `json:"hook_ready"`
	AgentsBlockOK   bool   `json:"agents_block_ready"`
	GeneratorsAllow int    `json:"generators_allow_count"`
	Notes           []string `json:"notes,omitempty"`
	Errors          []string `json:"errors,omitempty"`
}

// Run gathers diagnostics for cwd/rootOverride.
func Run(cfg config.Config, cfgPath, cwd, rootOverride string, refresh bool) Report {
	rep := Report{
		Version:         version.Version,
		Commit:          version.Commit,
		BuildTime:       version.BuildTime,
		ConfigPath:      cfgPath,
		HookReady:       true,
		AgentsBlockOK:   true,
		GeneratorsAllow: len(cfg.Generators.Allow),
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

	if len(cfg.Generators.Allow) == 0 {
		rep.Notes = append(rep.Notes, "generators.allow empty; exec will reject all generators")
	}

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
	fmt.Fprintf(&b, "git_available: %v\nhook_ready: %v\nagents_block_ready: %v\n", r.GitAvailable, r.HookReady, r.AgentsBlockOK)
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
