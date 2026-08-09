package files

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
