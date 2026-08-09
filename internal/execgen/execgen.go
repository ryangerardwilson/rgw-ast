package execgen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ryangerardwilson/rgw-ast/internal/config"
	"github.com/ryangerardwilson/rgw-ast/internal/files"
	"github.com/ryangerardwilson/rgw-ast/internal/measure"
)

// Change is one path altered by a generator.
type Change struct {
	Path   string `json:"path"`
	Status string `json:"status"` // created|modified|deleted|unknown
	Hash   string `json:"hash,omitempty"`
}

// Report is the result of a trusted generator run.
type Report struct {
	Root       string   `json:"root"`
	Command    []string `json:"command"`
	ExitCode   int      `json:"exit_code"`
	Before     string   `json:"before_porcelain"`
	After      string   `json:"after_porcelain"`
	Changes    []Change `json:"changes"`
	AllowedBy  string   `json:"allowed_by,omitempty"`
	Enforced   bool     `json:"enforced"`
}

// Allowed reports whether cmdline matches a trusted generator pattern.
func Allowed(cfg config.Config, cmdline string) (bool, string) {
	cmdline = strings.TrimSpace(cmdline)
	low := strings.ToLower(cmdline)
	for _, pat := range cfg.Generators.Allow {
		p := strings.TrimSpace(pat)
		if p == "" {
			continue
		}
		pl := strings.ToLower(p)
		if strings.HasPrefix(low, pl) || strings.Contains(low, " "+pl) {
			return true, p
		}
		// bare openspec as first token
		if strings.HasPrefix(pl, "openspec") && (strings.HasPrefix(low, "openspec ") || low == "openspec") {
			return true, p
		}
	}
	return false, ""
}

// Run executes argv under root after verifying allowlist; returns report.
func Run(root string, cfg config.Config, argv []string, enforced bool) (Report, error) {
	rep := Report{Root: root, Command: argv, Enforced: enforced}
	if len(argv) == 0 {
		return rep, fmt.Errorf("exec requires a command after --")
	}
	cmdline := strings.Join(argv, " ")
	ok, by := Allowed(cfg, cmdline)
	if !ok {
		return rep, fmt.Errorf("command not in generators.allow: %q", cmdline)
	}
	rep.AllowedBy = by

	before, _ := measure.GitPorcelain(root)
	rep.Before = before

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			rep.ExitCode = ee.ExitCode()
		} else {
			rep.ExitCode = 1
			return rep, err
		}
	}

	after, _ := measure.GitPorcelain(root)
	rep.After = after
	rep.Changes = diffPorcelain(root, before, after)
	return rep, nil
}

func diffPorcelain(root, before, after string) []Change {
	bset := porcelainPaths(before)
	aset := porcelainPaths(after)
	// union of paths that appear in either with status from after when present
	seen := map[string]bool{}
	var out []Change
	for path, st := range aset {
		seen[path] = true
		c := Change{Path: path, Status: statusLabel(st, bset[path])}
		abs := filepath.Join(root, path)
		if st != "D " && st != " D" && !strings.HasPrefix(st, "D") {
			if h, err := files.HashFile(abs); err == nil {
				c.Hash = h
			}
		} else {
			c.Status = "deleted"
		}
		if _, was := bset[path]; !was && c.Status != "deleted" {
			c.Status = "created"
		} else if c.Status == "" {
			c.Status = "modified"
		}
		out = append(out, c)
	}
	for path := range bset {
		if seen[path] {
			continue
		}
		// disappeared from dirty list after — may be cleaned
		out = append(out, Change{Path: path, Status: "cleaned"})
	}
	return out
}

func porcelainPaths(porc string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(porc, "\n") {
		if len(line) < 4 {
			continue
		}
		st := line[:2]
		path := strings.TrimSpace(line[3:])
		if i := strings.Index(path, " -> "); i >= 0 {
			path = path[i+4:]
		}
		path = strings.Trim(path, "\"")
		m[path] = st
	}
	return m
}

func statusLabel(after, before string) string {
	if before == "" {
		return "created"
	}
	if strings.Contains(after, "D") {
		return "deleted"
	}
	return "modified"
}
