package execgen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ryangerardwilson/rgw-ast/internal/config"
	"github.com/ryangerardwilson/rgw-ast/internal/files"
	"github.com/ryangerardwilson/rgw-ast/internal/measure"
)

// Change is one path altered by a generator relative to pre-run state.
type Change struct {
	Path   string `json:"path"`
	Status string `json:"status"` // created|modified|deleted|created_ignored
	Hash   string `json:"hash,omitempty"`
}

// Report is the result of a trusted generator run.
type Report struct {
	Root             string   `json:"root"`
	Command          []string `json:"command"`
	ExitCode         int      `json:"exit_code"`
	BeforePorcelain  string   `json:"before_porcelain"`
	AfterPorcelain   string   `json:"after_porcelain"`
	PreExistingDirty []string `json:"pre_existing_dirty,omitempty"`
	Changes          []Change `json:"changes"`
	IgnoredObserved  []Change `json:"ignored_observed,omitempty"`
	AllowedBy        string   `json:"allowed_by,omitempty"`
	Enforced         bool     `json:"enforced"`
	AuditComplete    bool     `json:"audit_complete"`
	AuditNotes       []string `json:"audit_notes,omitempty"`
}

// Run executes argv under root after verifying allowlist; returns report.
func Run(root string, cfg config.Config, argv []string, enforced bool) (Report, error) {
	rep := Report{Root: root, Command: argv, Enforced: enforced}
	if len(argv) == 0 {
		return rep, fmt.Errorf("exec requires a command after --")
	}
	ok, by := Allowed(cfg, argv)
	if !ok {
		return rep, fmt.Errorf("command not in generators.allow (structural prefix from argv[0]): %q", strings.Join(argv, " "))
	}
	rep.AllowedBy = by

	beforeText, beforeErr := measure.GitPorcelain(root)
	before := snapFromPorcelain(root, beforeText)
	if beforeErr != nil {
		rep.AuditNotes = append(rep.AuditNotes, "before porcelain unavailable: "+beforeErr.Error())
	}
	rep.BeforePorcelain = beforeText
	for p := range before {
		rep.PreExistingDirty = append(rep.PreExistingDirty, p)
	}
	sort.Strings(rep.PreExistingDirty)

	// content identity of pre-existing paths for change detection
	beforeHash := map[string]string{}
	for p, e := range before {
		if e.Hash != "" {
			beforeHash[p] = e.Hash
		}
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	runErr := cmd.Run()
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			rep.ExitCode = ee.ExitCode()
		} else {
			rep.ExitCode = 1
			return rep, runErr
		}
	}

	afterText, afterErr := measure.GitPorcelain(root)
	after := snapFromPorcelain(root, afterText)
	if afterErr != nil {
		rep.AuditNotes = append(rep.AuditNotes, "after porcelain unavailable: "+afterErr.Error())
	}
	rep.AfterPorcelain = afterText
	rep.AuditComplete = beforeErr == nil && afterErr == nil
	if !rep.AuditComplete {
		rep.AuditNotes = append(rep.AuditNotes, "audit incomplete without git porcelain")
	}

	rep.Changes = delta(root, before, after, beforeHash)

	// ignored new files
	ign, notes := observeIgnored(root, beforeHash)
	rep.IgnoredObserved = ign
	rep.AuditNotes = append(rep.AuditNotes, notes...)

	return rep, nil
}

type entry struct {
	Status string
	Hash   string
}

func snapFromPorcelain(root, text string) map[string]entry {
	m := map[string]entry{}
	for path, st := range measure.ParsePorcelainPaths(text) {
		e := entry{Status: st}
		if !strings.Contains(st, "D") {
			if h, err := files.HashFile(filepath.Join(root, path)); err == nil {
				e.Hash = h
			}
		}
		m[path] = e
	}
	return m
}

func delta(root string, before, after map[string]entry, beforeHash map[string]string) []Change {
	var out []Change
	for path, ae := range after {
		be, ok := before[path]
		if !ok {
			// new dirty path
			c := Change{Path: path, Status: "created", Hash: ae.Hash}
			if c.Hash == "" {
				if h, err := files.HashFile(filepath.Join(root, path)); err == nil {
					c.Hash = h
				}
			}
			// if it was untracked and still untracked with same hash - shouldn't be in before
			out = append(out, c)
			continue
		}
		// pre-existing dirty: only report if content hash changed
		bh := beforeHash[path]
		if bh == "" {
			bh = be.Hash
		}
		ah := ae.Hash
		if ah == "" {
			if h, err := files.HashFile(filepath.Join(root, path)); err == nil {
				ah = h
			}
		}
		if bh != "" && ah != "" && bh != ah {
			out = append(out, Change{Path: path, Status: "modified", Hash: ah})
		}
		// same content: do not attribute to generator
	}
	for path := range before {
		if _, ok := after[path]; ok {
			continue
		}
		abs := filepath.Join(root, path)
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			out = append(out, Change{Path: path, Status: "deleted"})
		}
	}
	// Detect brand-new tracked files that are clean after generator (not in porcelain):
	// compare file existence via git ls-files --others is hard; use find new files with git status only.
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func observeIgnored(root string, beforeHash map[string]string) ([]Change, []string) {
	cmd := exec.Command("git", "-C", root, "status", "--porcelain=v1", "--ignored", "-uall")
	out, err := cmd.Output()
	if err != nil {
		return nil, []string{"ignored observation unavailable"}
	}
	var changes []Change
	for path, st := range measure.ParsePorcelainPaths(string(out)) {
		if !strings.Contains(st, "!") {
			continue
		}
		if _, ok := beforeHash[path]; ok {
			continue
		}
		c := Change{Path: path, Status: "created_ignored"}
		if h, err := files.HashFile(filepath.Join(root, path)); err == nil {
			c.Hash = h
		}
		changes = append(changes, c)
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	if len(changes) == 0 {
		return nil, nil
	}
	return changes, []string{"ignored paths listed in ignored_observed (not hash-guarded by default)"}
}
