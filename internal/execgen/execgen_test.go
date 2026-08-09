package execgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ryangerardwilson/rgw-ast/internal/config"
	"github.com/ryangerardwilson/rgw-ast/internal/measure"
)

func TestParsePorcelainLeadingSpace(t *testing.T) {
	text := " M sub/a.go\n?? new.txt\n"
	m := measure.ParsePorcelainPaths(text)
	if _, ok := m["sub/a.go"]; !ok {
		t.Fatalf("got %#v", m)
	}
	if _, ok := m["new.txt"]; !ok {
		t.Fatalf("got %#v", m)
	}
	if _, ok := m["ub/a.go"]; ok {
		t.Fatal("corrupted path")
	}
}

func TestExecDeltaIgnoresUnchangedPreDirty(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "init", root).Run(); err != nil {
		t.Skip(err)
	}
	_ = exec.Command("git", "-C", root, "config", "user.email", "t@t").Run()
	_ = exec.Command("git", "-C", root, "config", "user.name", "t").Run()
	must(t, os.WriteFile(filepath.Join(root, "keep.go"), []byte("package k\n"), 0o644))
	_ = exec.Command("git", "-C", root, "add", "keep.go").Run()
	_ = exec.Command("git", "-C", root, "commit", "-m", "i").Run()
	// pre-dirty untouched file
	must(t, os.WriteFile(filepath.Join(root, "dirty.go"), []byte("package d\n"), 0o644))

	cfg := config.Default()
	// use true as allowlisted "generator"
	cfg.Generators.Allow = []string{"true"}
	rep, err := Run(root, cfg, []string{"true"}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range rep.Changes {
		if c.Path == "dirty.go" {
			t.Fatalf("should not attribute pre-existing dirty: %+v", rep.Changes)
		}
	}
	// create file via generator
	cfg.Generators.Allow = []string{"touch"}
	rep, err = Run(root, cfg, []string{"touch", "generated.txt"}, false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range rep.Changes {
		if c.Path == "generated.txt" && c.Status == "created" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected generated.txt created: %+v", rep)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
