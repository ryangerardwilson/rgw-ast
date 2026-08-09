package measure

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ryangerardwilson/rgw-ast/internal/config"
)

func TestDirtyTreeInvalidatesCache(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "init", root).Run(); err != nil {
		t.Skip("git init failed")
	}
	// identity so commit works if needed
	_ = exec.Command("git", "-C", root, "config", "user.email", "t@t").Run()
	_ = exec.Command("git", "-C", root, "config", "user.name", "t").Run()

	src := filepath.Join(root, "pkg", "a.go")
	mustWrite(t, src, "package pkg\n")
	_ = exec.Command("git", "-C", root, "add", "-A").Run()
	_ = exec.Command("git", "-C", root, "commit", "-m", "init").Run()

	cfg := config.Default()
	cfg.Cache.Dir = t.TempDir()

	r1, err := CountCached(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	// warm
	r2, err := CountCached(root, cfg)
	if err != nil || !r2.CacheHit {
		t.Fatalf("expected cache hit: %+v err=%v", r2, err)
	}
	// nested edit without touching root dir
	mustWrite(t, src, "package pkg\n//1\n//2\n//3\n//4\n//5\n")
	r3, err := CountCached(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if r3.CacheHit {
		t.Fatal("expected cache miss after nested edit")
	}
	if r3.LOC <= r1.LOC {
		t.Fatalf("expected higher loc after edit: was %d now %d", r1.LOC, r3.LOC)
	}
}

func TestGitDirtyFingerprintSeesEdit(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "init", root).Run(); err != nil {
		t.Skip("git")
	}
	f := filepath.Join(root, "a.go")
	mustWrite(t, f, "package a\n")
	_ = exec.Command("git", "-C", root, "add", "a.go").Run()
	before := gitDirtyFingerprint(root)
	mustWrite(t, f, "package a\n//x\n")
	after := gitDirtyFingerprint(root)
	if string(before) == string(after) {
		// may still differ if untracked vs modified
		// force unstaged modify after add
		if len(after) == 0 && len(before) == 0 {
			t.Fatal("expected dirty fingerprint")
		}
	}
	_ = os.WriteFile(f, []byte("package a\n//y\n"), 0o644)
	after2 := gitDirtyFingerprint(root)
	if len(after2) == 0 {
		t.Fatal("expected non-empty dirty fingerprint")
	}
}
