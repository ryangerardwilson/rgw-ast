package measure

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ryangerardwilson/rgw-ast/internal/config"
)

// Result is a LOC measure for one workspace root.
type Result struct {
	Root                string
	LOC                 int
	FileCount           int
	NestedReposSkipped   int
	GitIgnoreActive     bool
	CacheHit            bool
	CacheAgeMs          int64
}

// PathMatches reports whether rel (slash-separated, relative to root) is
// included by cfg: matches include and does not match exclude.
func PathMatches(cfg config.Config, rel string) bool {
	rel = filepath.ToSlash(rel)
	include := compileGlobs(cfg.Include)
	exclude := compileGlobs(cfg.Exclude)
	if matchAny(exclude, rel) {
		return false
	}
	return matchAny(include, rel)
}

// Count walks root with include/exclude from cfg and counts physical lines.
func Count(root string, cfg config.Config) (Result, error) {
	res := Result{Root: root}
	include := compileGlobs(cfg.Include)
	exclude := compileGlobs(cfg.Exclude)
	gi := loadGitIgnore(root)
	res.GitIgnoreActive = gi != nil
	rootAbs, _ := filepath.Abs(root)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(rootAbs, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			if SkipDirName(base) {
				return filepath.SkipDir
			}
			// Nested git repository: skip (but not the workspace root itself).
			if isNestedGitRoot(path, rootAbs) {
				res.NestedReposSkipped++
				return filepath.SkipDir
			}
			if matchAny(exclude, rel+"/") || matchAny(exclude, rel) {
				return filepath.SkipDir
			}
			if gi != nil && gi.ignored(rel, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if matchAny(exclude, rel) {
			return nil
		}
		if gi != nil && gi.ignored(rel, false) {
			return nil
		}
		if !matchAny(include, rel) {
			return nil
		}
		n, ok, err := countFileLines(path)
		if err != nil || !ok {
			return nil
		}
		res.LOC += n
		res.FileCount++
		return nil
	})
	return res, err
}

func isNestedGitRoot(path, workspaceRoot string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	ws, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return false
	}
	if abs == ws {
		return false
	}
	gitPath := filepath.Join(abs, ".git")
	st, err := os.Stat(gitPath)
	if err != nil {
		return false
	}
	return st.IsDir() || st.Mode().IsRegular()
}

// SkipDirName is a fast path for directory basenames that never contribute
// source under default policy (and are huge on real machines).
// Note: hidden dirs are NOT blanket-skipped so .bashrc.d etc. remain visible.
func SkipDirName(base string) bool {
	switch base {
	case ".git", ".hg", ".svn", ".jj",
		"node_modules", "vendor", "dist", "build", "target", "coverage",
		".next", ".nuxt", ".turbo", ".cache", ".parcel-cache",
		".npm", ".yarn", ".pnpm-store", ".bun",
		".cargo", ".rustup",
		".venv", "venv", "__pycache__", ".mypy_cache", ".pytest_cache",
		".tox", ".eggs",
		".idea", ".vscode", ".gradle",
		"Android",
		"chromium":
		return true
	default:
		return false
	}
}

func countFileLines(path string) (int, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false, err
	}
	defer f.Close()

	const maxProbe = 512 * 1024
	buf := make([]byte, maxProbe)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return 0, false, err
	}
	probe := buf[:n]
	if bytes.IndexByte(probe, 0) >= 0 {
		return 0, false, nil
	}

	lines := bytes.Count(probe, []byte{'\n'})
	rest, err := io.ReadAll(f)
	if err != nil {
		return 0, false, err
	}
	if bytes.IndexByte(rest, 0) >= 0 {
		return 0, false, nil
	}
	lines += bytes.Count(rest, []byte{'\n'})

	totalSize := int64(n) + int64(len(rest))
	if totalSize == 0 {
		return 0, true, nil
	}
	var last byte
	if len(rest) > 0 {
		last = rest[len(rest)-1]
	} else if n > 0 {
		last = probe[n-1]
	}
	if last != '\n' {
		lines++
	}
	return lines, true, nil
}

type glob struct {
	raw      string
	patterns []string
}

func compileGlobs(raw []string) []glob {
	out := make([]glob, 0, len(raw))
	for _, r := range raw {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		out = append(out, glob{raw: r, patterns: expandBraces(r)})
	}
	return out
}

func matchAny(globs []glob, rel string) bool {
	for _, g := range globs {
		for _, p := range g.patterns {
			if matchGlob(p, rel) {
				return true
			}
		}
	}
	return false
}

func expandBraces(s string) []string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return []string{s}
	}
	end := strings.IndexByte(s[start:], '}')
	if end < 0 {
		return []string{s}
	}
	end += start
	inner := s[start+1 : end]
	parts := strings.Split(inner, ",")
	prefix := s[:start]
	suffix := s[end+1:]
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, expandBraces(prefix+p+suffix)...)
	}
	return out
}

func matchGlob(pattern, name string) bool {
	pattern = filepath.ToSlash(pattern)
	name = filepath.ToSlash(name)
	return matchGlobRec(pattern, name)
}

func matchGlobRec(pattern, name string) bool {
	for {
		if pattern == "" {
			return name == ""
		}
		if strings.HasPrefix(pattern, "**/") {
			rest := pattern[3:]
			if matchGlobRec(rest, name) {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if i == 0 || name[i-1] == '/' {
					if matchGlobRec(rest, name[i:]) {
						return true
					}
				}
			}
			if rest == "" {
				return true
			}
			return false
		}
		if pattern == "**" {
			return true
		}
		if pattern[0] == '*' {
			pattern = pattern[1:]
			for i := 0; i <= len(name); i++ {
				if i > 0 && name[i-1] == '/' {
					break
				}
				if matchGlobRec(pattern, name[i:]) {
					return true
				}
				if i < len(name) && name[i] == '/' {
					break
				}
			}
			return false
		}
		if name == "" {
			return false
		}
		if pattern[0] == '?' {
			if name[0] == '/' {
				return false
			}
			pattern = pattern[1:]
			name = name[1:]
			continue
		}
		if pattern[0] != name[0] {
			return false
		}
		pattern = pattern[1:]
		name = name[1:]
	}
}
