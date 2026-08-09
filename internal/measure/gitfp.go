package measure

import (
	"bytes"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func runGit(root string, args ...string) []byte {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return out
}

// sampleNestedMtimes hashes mtimes of immediate children and one level deeper
// as a fallback when git is unavailable.
func sampleNestedMtimes(root string) []byte {
	h := sha256.New()
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return nil
		}
		// depth limit 3
		if depth := pathDepth(rel); depth > 3 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() && SkipDirName(d.Name()) {
			return filepath.SkipDir
		}
		if d.IsDir() && path != root {
			if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
				return filepath.SkipDir
			}
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		_, _ = h.Write([]byte(rel))
		var t time.Time
		t = info.ModTime()
		var b [8]byte
		nsec := t.UnixNano()
		for i := 0; i < 8; i++ {
			b[i] = byte(nsec >> (8 * i))
		}
		_, _ = h.Write(b[:])
		return nil
	})
	return h.Sum(nil)
}

func pathDepth(rel string) int {
	if rel == "" || rel == "." {
		return 0
	}
	n := 1
	for i := 0; i < len(rel); i++ {
		if rel[i] == '/' || rel[i] == filepath.Separator {
			n++
		}
	}
	return n
}

// GitPorcelain returns porcelain status for exec snapshots.
func GitPorcelain(root string) (string, error) {
	out := runGit(root, "status", "--porcelain=v1", "-uall")
	if out == nil {
		return "", os.ErrNotExist
	}
	return string(bytes.TrimSpace(out)), nil
}
