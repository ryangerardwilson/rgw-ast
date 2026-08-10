package files

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Op is one exact text replacement.
type Op struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// CreateAbsent writes content only if path does not exist.
func CreateAbsent(path string, content []byte, parents bool, mode os.FileMode) (string, error) {
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("path already exists: %s", path)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if parents {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
	} else {
		if _, err := os.Stat(filepath.Dir(path)); err != nil {
			return "", fmt.Errorf("parent directory missing (use --parents): %w", err)
		}
	}
	if mode == 0 {
		mode = 0o644
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".rgw-ast-create-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", err
	}
	return HashFile(path)
}

// AppendExact appends content when hash matches.
func AppendExact(path, expectHash string, content []byte) (string, error) {
	cur, err := HashFile(path)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(cur, expectHash) {
		return "", fmt.Errorf("hash mismatch: expected %s, got %s", expectHash, cur)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	updated := append(append([]byte{}, data...), content...)
	return writeAtomic(path, updated)
}

// PatchOps applies ordered unique replacements against one expected hash.
func PatchOps(path, expectHash string, ops []Op) (string, error) {
	if len(ops) == 0 {
		return "", fmt.Errorf("no patch operations")
	}
	cur, err := HashFile(path)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(cur, expectHash) {
		return "", fmt.Errorf("hash mismatch: expected %s, got %s", expectHash, cur)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(data)
	for i, op := range ops {
		if op.Old == "" {
			return "", fmt.Errorf("op %d: old must not be empty", i)
		}
		count := strings.Count(content, op.Old)
		if count == 0 {
			return "", fmt.Errorf("op %d: old string not found", i)
		}
		if count > 1 {
			return "", fmt.Errorf("op %d: old string matches %d times; must be unique", i, count)
		}
		content = strings.Replace(content, op.Old, op.New, 1)
	}
	return writeAtomic(path, []byte(content))
}

// ParseOpsFile loads JSON array of ops.
func ParseOpsFile(path string) ([]Op, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var ops []Op
	if err := json.Unmarshal(data, &ops); err != nil {
		return nil, err
	}
	return ops, nil
}

// DeleteExact removes one verified regular file and optionally prunes empty
// ancestors without ever removing the workspace root.
func DeleteExact(root, path, expectHash string, pruneEmpty bool) ([]string, error) {
	if expectHash == "" {
		return nil, fmt.Errorf("expected hash is required")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	abs, err := ResolvePath(rootAbs, path)
	if err != nil {
		return nil, err
	}
	if abs == rootAbs {
		return nil, fmt.Errorf("workspace root cannot be deleted")
	}
	for dir := filepath.Dir(abs); dir != rootAbs; dir = filepath.Dir(dir) {
		info, err := os.Lstat(dir)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("deletion path contains symlink ancestor: %s", dir)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("deletion parent is not a directory: %s", dir)
		}
	}

	rootResolved, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	parentResolved, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return nil, fmt.Errorf("resolve deletion parent: %w", err)
	}
	inside, err := filepath.Rel(rootResolved, parentResolved)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("path %q resolves outside workspace root", path)
	}

	info, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("delete requires a regular file: %s", path)
	}
	cur, err := HashFile(abs)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(cur, expectHash) {
		return nil, fmt.Errorf("hash mismatch: expected %s, got %s", expectHash, cur)
	}
	if err := os.Remove(abs); err != nil {
		return nil, err
	}
	if !pruneEmpty {
		return nil, nil
	}
	return pruneEmptyAncestors(rootAbs, filepath.Dir(abs))
}

func pruneEmptyAncestors(root, start string) ([]string, error) {
	var pruned []string
	for dir := start; dir != root; dir = filepath.Dir(dir) {
		inside, err := filepath.Rel(root, dir)
		if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
			return pruned, fmt.Errorf("prune path escapes workspace root: %s", dir)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return pruned, err
		}
		if len(entries) != 0 {
			break
		}
		if err := os.Remove(dir); err != nil {
			if errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, syscall.EEXIST) {
				break
			}
			return pruned, err
		}
		pruned = append(pruned, dir)
	}
	return pruned, nil
}

// ReadAllFile is os.ReadFile with clearer errors.
func ReadAllFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// ReadStdin reads all of r.
func ReadStdin(r io.Reader) ([]byte, error) {
	return io.ReadAll(r)
}

func writeAtomic(path string, content []byte) (string, error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".rgw-ast-write-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(fileMode(path)); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", err
	}
	return HashFile(path)
}
