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
	Root      string
	LOC       int
	FileCount int
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

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// Skip unreadable nodes.
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			base := d.Name()
			// Skip VCS, package, and build trees quickly.
			if SkipDirName(base) {
				return filepath.SkipDir
			}
			// If the directory path matches exclude as a prefix pattern, skip.
			if matchAny(exclude, rel+"/") || matchAny(exclude, rel) {
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

// SkipDirName is a fast path for directory basenames that never contribute
// source under default policy (and are huge on real machines).
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
		"Android", // SDK trees when nested under home workspaces
		"chromium": // browser profile blobs if nested
		return true
	}
	// Hidden directories (except those already handled) are rarely project source
	// and dominate home-as-git-root scans.
	if strings.HasPrefix(base, ".") {
		return true
	}
	return false
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

	// Count newlines in probe + rest of file.
	lines := bytes.Count(probe, []byte{'\n'})
	// If file has content and does not end with newline, last line still counts
	// when we finish reading; handle below.

	rest, err := io.ReadAll(f)
	if err != nil {
		return 0, false, err
	}
	if bytes.IndexByte(rest, 0) >= 0 {
		return 0, false, nil
	}
	lines += bytes.Count(rest, []byte{'\n'})

	// Combine for trailing partial line.
	totalSize := int64(n) + int64(len(rest))
	if totalSize == 0 {
		return 0, true, nil
	}
	// If last byte is not newline, count the final line.
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
	raw string
	// simplified matching: ** and * and {a,b} brace expansion
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

// expandBraces expands a single {a,b,c} segment once (enough for our defaults).
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

// matchGlob supports ** (any path including /), * (within segment-ish), and exact.
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
			// empty match for **
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
			// also match ** at end of path component stream
			if rest == "" {
				return true
			}
			return false
		}
		if pattern == "**" {
			return true
		}
		if pattern[0] == '*' {
			// * does not cross /
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
