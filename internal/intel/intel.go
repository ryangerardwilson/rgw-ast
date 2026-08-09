package intel

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ryangerardwilson/rgw-ast/internal/config"
	"github.com/ryangerardwilson/rgw-ast/internal/measure"
)

// Entry is one outline line.
type Entry struct {
	Path  string
	Kind  string
	Name  string
	Line  int
	End   int
	Label string
}

// Map outlines path (file or directory) under root.
func Map(root, relOrAbs string, cfg config.Config, maxEntries int) ([]Entry, error) {
	if maxEntries <= 0 {
		maxEntries = cfg.MaxMapEntries
	}
	target := root
	if relOrAbs != "" {
		p, err := resolve(root, relOrAbs)
		if err != nil {
			return nil, err
		}
		target = p
	}
	st, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	var entries []Entry
	if !st.IsDir() {
		rel, _ := filepath.Rel(root, target)
		es, err := mapFile(root, target, filepath.ToSlash(rel))
		if err != nil {
			return nil, err
		}
		return es, nil
	}

	err = filepath.WalkDir(target, func(path string, d os.DirEntry, err error) error {
		if err != nil || len(entries) >= maxEntries {
			if len(entries) >= maxEntries {
				return filepath.SkipAll
			}
			return nil
		}
		if d.IsDir() {
			if measure.SkipDirName(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !pathIncluded(cfg, rel) {
			return nil
		}
		es, err := mapFile(root, path, rel)
		if err != nil {
			return nil
		}
		for _, e := range es {
			entries = append(entries, e)
			if len(entries) >= maxEntries {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		return entries, err
	}
	return entries, nil
}

// Show finds a symbol by name or path:name and returns its body.
func Show(root, target string, cfg config.Config) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("show requires a target")
	}
	pathHint := ""
	name := target
	if i := strings.LastIndex(target, ":"); i >= 0 {
		pathHint = target[:i]
		name = target[i+1:]
	}

	var matches []Entry
	var bodies []string

	collect := func(path, rel string) {
		es, err := mapFile(root, path, rel)
		if err != nil {
			return
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return
		}
		lines := strings.Split(string(src), "\n")
		for _, e := range es {
			if e.Name != name {
				continue
			}
			if pathHint != "" {
				hint := filepath.ToSlash(pathHint)
				if rel != hint && !strings.HasSuffix(rel, "/"+hint) && rel != strings.TrimPrefix(hint, "./") {
					if !strings.Contains(rel, hint) {
						continue
					}
				}
			}
			body := sliceLines(lines, e.Line, e.End)
			matches = append(matches, e)
			bodies = append(bodies, body)
		}
	}

	if pathHint != "" {
		p, err := resolve(root, pathHint)
		if err == nil {
			rel, _ := filepath.Rel(root, p)
			collect(p, filepath.ToSlash(rel))
		}
	}

	if len(matches) == 0 {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if measure.SkipDirName(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if !pathIncluded(cfg, rel) {
				return nil
			}
			collect(path, rel)
			if len(matches) > 20 {
				return filepath.SkipAll
			}
			return nil
		})
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("symbol %q not found", name)
	}
	if len(matches) > 1 {
		var b strings.Builder
		fmt.Fprintf(&b, "ambiguous symbol %q (%d matches):\n", name, len(matches))
		for _, m := range matches {
			fmt.Fprintf(&b, "  %s:%s (line %d)\n", m.Path, m.Name, m.Line)
		}
		fmt.Fprintf(&b, "disambiguate with path:symbol")
		return "", fmt.Errorf("%s", b.String())
	}
	return bodies[0], nil
}

// Hit is a search result.
type Hit struct {
	Path    string
	Line    int
	Content string
}

// SearchOpts filters search.
type SearchOpts struct {
	PathPrefix string // relative dir or file under root
	Glob       string // optional extra glob like *.sh
}

// Search finds literal substring matches, capped.
func Search(root, query string, cfg config.Config) ([]Hit, bool, error) {
	return SearchOptsRun(root, query, cfg, SearchOpts{})
}

// SearchOptsRun is Search with filters.
func SearchOptsRun(root, query string, cfg config.Config, opts SearchOpts) ([]Hit, bool, error) {
	if query == "" {
		return nil, false, fmt.Errorf("search requires a query")
	}
	max := cfg.Enforcement.MaxSearchHits
	if max <= 0 {
		max = 50
	}
	start := root
	if opts.PathPrefix != "" {
		p := opts.PathPrefix
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		start = filepath.Clean(p)
	}
	var extra []measureGlob
	_ = extra
	var hits []Hit
	truncated := false
	err := filepath.WalkDir(start, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if measure.SkipDirName(d.Name()) {
				return filepath.SkipDir
			}
			// nested git
			if path != root {
				if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		// Direct path under --path may map even if not in include when it's a single file map;
		// for search still prefer include match OR under explicit path prefix.
		if opts.PathPrefix == "" && !pathIncluded(cfg, rel) {
			return nil
		}
		if opts.PathPrefix != "" && !pathIncluded(cfg, rel) {
			// allow shell/config when explicitly scoped under path
			if !strings.HasSuffix(rel, ".sh") && !strings.Contains(rel, ".bashrc") {
				// still try include
				if !pathIncluded(cfg, rel) {
					// allow any text file under explicit path
				}
			}
		}
		if opts.Glob != "" {
			ok, _ := filepath.Match(opts.Glob, filepath.Base(rel))
			ok2, _ := filepath.Match(opts.Glob, rel)
			if !ok && !ok2 {
				// try ** style
				if !matchSimpleGlob(opts.Glob, rel) {
					return nil
				}
			}
		}
		data, err := os.ReadFile(path)
		if err != nil || strings.Contains(string(data), "\x00") {
			return nil
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, query) {
				hits = append(hits, Hit{Path: rel, Line: i + 1, Content: truncateSnippet(strings.TrimRight(line, "\r"), 120)})
				if len(hits) >= max {
					truncated = true
					return filepath.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		return hits, truncated, err
	}
	return hits, truncated, nil
}

// Explain describes why a path is visible or not.
func Explain(root, relOrAbs string, cfg config.Config) (string, error) {
	abs, err := resolve(root, relOrAbs)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	var b strings.Builder
	fmt.Fprintf(&b, "path: %s\n", rel)
	fmt.Fprintf(&b, "abs: %s\n", abs)
	st, err := os.Stat(abs)
	if err != nil {
		fmt.Fprintf(&b, "exists: false\n")
		fmt.Fprintf(&b, "error: %v\n", err)
		return b.String(), nil
	}
	fmt.Fprintf(&b, "exists: true\n")
	fmt.Fprintf(&b, "is_dir: %v\n", st.IsDir())
	included := pathIncluded(cfg, rel)
	fmt.Fprintf(&b, "include_match: %v\n", included)
	if bin, err := isBin(abs); err == nil {
		fmt.Fprintf(&b, "binary: %v\n", bin)
	}
	// nested git ancestor?
	nested := false
	dir := abs
	if !st.IsDir() {
		dir = filepath.Dir(abs)
	}
	for {
		if dir == root || dir == filepath.Dir(dir) {
			break
		}
		if dir != root {
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				nested = true
				fmt.Fprintf(&b, "under_nested_git: %s\n", dir)
				break
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if !nested {
		fmt.Fprintf(&b, "under_nested_git: false\n")
	}
	fmt.Fprintf(&b, "map_visible: %v\n", !st.IsDir())
	fmt.Fprintf(&b, "search_visible: %v\n", included && !st.IsDir())
	return b.String(), nil
}

func isBin(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	return strings.Contains(string(buf[:n]), "\x00"), nil
}

func matchSimpleGlob(pattern, name string) bool {
	// reuse filepath match on full path segments
	ok, _ := filepath.Match(pattern, name)
	if ok {
		return true
	}
	ok, _ = filepath.Match(pattern, filepath.Base(name))
	return ok
}

// silence unused type if any
type measureGlob struct{}

func resolve(root, path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	return filepath.Clean(filepath.Join(root, path)), nil
}

func pathIncluded(cfg config.Config, rel string) bool {
	return measure.PathMatches(cfg, rel)
}

func mapFile(root, abs, rel string) ([]Entry, error) {
	base := filepath.Base(rel)
	if strings.HasSuffix(rel, ".go") {
		return mapGo(abs, rel)
	}
	if strings.HasSuffix(rel, ".sh") || strings.HasSuffix(rel, ".bash") ||
		base == ".bashrc" || base == ".bash_profile" || base == ".profile" {
		return mapBash(abs, rel)
	}
	return mapHeuristic(abs, rel)
}

var (
	reBashFunc1 = regexp.MustCompile(`(?m)^[ \t]*(?:function[ \t]+)?([A-Za-z_][A-Za-z0-9_]*)[ \t]*\([ \t]*\)[ \t]*\{`)
	reBashFunc2 = regexp.MustCompile(`(?m)^[ \t]*function[ \t]+([A-Za-z_][A-Za-z0-9_]*)[ \t]*\{`)
	reBashAlias = regexp.MustCompile(`(?m)^[ \t]*alias[ \t]+([A-Za-z_][A-Za-z0-9_]*)=`)
)

func mapBash(abs, rel string) ([]Entry, error) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	text := string(data)
	lines := strings.Split(text, "\n")
	var entries []Entry
	seen := map[string]bool{}
	add := func(kind, name string, line int) {
		key := kind + ":" + name + ":" + fmt.Sprint(line)
		if seen[key] {
			return
		}
		seen[key] = true
		end := line + 20
		if end > len(lines) {
			end = len(lines)
		}
		entries = append(entries, Entry{
			Path: rel, Kind: kind, Name: name, Line: line, End: end,
			Label: fmt.Sprintf("%s %s", kind, name),
		})
	}
	for i, line := range lines {
		ln := i + 1
		if m := reBashFunc1.FindStringSubmatch(line); len(m) == 2 {
			add("function", m[1], ln)
			continue
		}
		if m := reBashFunc2.FindStringSubmatch(line); len(m) == 2 {
			add("function", m[1], ln)
			continue
		}
		if m := reBashAlias.FindStringSubmatch(line); len(m) == 2 {
			add("alias", m[1], ln)
		}
	}
	if len(entries) == 0 {
		return mapHeuristic(abs, rel)
	}
	return entries, nil
}

func mapGo(abs, rel string) ([]Entry, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, abs, nil, parser.ParseComments)
	if err != nil {
		return mapHeuristic(abs, rel)
	}
	var entries []Entry
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name == nil {
				continue
			}
			pos := fset.Position(d.Pos())
			end := fset.Position(d.End())
			name := d.Name.Name
			kind := "func"
			if d.Recv != nil {
				kind = "method"
			}
			entries = append(entries, Entry{
				Path: rel, Kind: kind, Name: name,
				Line: pos.Line, End: end.Line,
				Label: fmt.Sprintf("%s %s", kind, name),
			})
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					pos := fset.Position(s.Pos())
					end := fset.Position(s.End())
					entries = append(entries, Entry{
						Path: rel, Kind: "type", Name: s.Name.Name,
						Line: pos.Line, End: end.Line,
						Label: fmt.Sprintf("type %s", s.Name.Name),
					})
				case *ast.ValueSpec:
					for _, n := range s.Names {
						pos := fset.Position(n.Pos())
						end := fset.Position(n.End())
						kind := "var"
						if d.Tok == token.CONST {
							kind = "const"
						}
						entries = append(entries, Entry{
							Path: rel, Kind: kind, Name: n.Name,
							Line: pos.Line, End: end.Line,
							Label: fmt.Sprintf("%s %s", kind, n.Name),
						})
					}
				}
			}
		}
	}
	return entries, nil
}

var (
	reFuncTS = regexp.MustCompile(`(?m)^(?:export\s+)?(?:async\s+)?function\s+([A-Za-z0-9_$]+)`)
	reClass  = regexp.MustCompile(`(?m)^(?:export\s+)?(?:abstract\s+)?class\s+([A-Za-z0-9_$]+)`)
	reDefPy  = regexp.MustCompile(`(?m)^(?:async\s+)?def\s+([A-Za-z0-9_]+)`)
	reClassPy = regexp.MustCompile(`(?m)^class\s+([A-Za-z0-9_]+)`)
	reFnRust = regexp.MustCompile(`(?m)^(?:pub\s+)?(?:async\s+)?fn\s+([A-Za-z0-9_]+)`)
)

func mapHeuristic(abs, rel string) ([]Entry, error) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	if strings.Contains(string(data), "\x00") {
		return nil, nil
	}
	text := string(data)
	lines := strings.Split(text, "\n")
	var entries []Entry
	add := func(kind, name string, line int) {
		end := line
		// crude: extend until blank line or next top-level-ish
		for i := line; i < len(lines); i++ {
			end = i + 1
			if i > line-1 && strings.TrimSpace(lines[i]) == "" && i+1 < len(lines) {
				// keep going a bit for short blocks
				if i > line+2 {
					break
				}
			}
		}
		// cap body window
		if end > line+80 {
			end = line + 80
		}
		entries = append(entries, Entry{
			Path: rel, Kind: kind, Name: name, Line: line, End: end,
			Label: fmt.Sprintf("%s %s", kind, name),
		})
	}
	for i, line := range lines {
		ln := i + 1
		if m := reFuncTS.FindStringSubmatch(line); len(m) == 2 {
			add("function", m[1], ln)
			continue
		}
		if m := reClass.FindStringSubmatch(line); len(m) == 2 {
			add("class", m[1], ln)
			continue
		}
		if m := reDefPy.FindStringSubmatch(line); len(m) == 2 {
			add("def", m[1], ln)
			continue
		}
		if m := reClassPy.FindStringSubmatch(line); len(m) == 2 {
			add("class", m[1], ln)
			continue
		}
		if m := reFnRust.FindStringSubmatch(line); len(m) == 2 {
			add("fn", m[1], ln)
			continue
		}
	}
	if len(entries) == 0 {
		// file-level placeholder
		entries = append(entries, Entry{
			Path: rel, Kind: "file", Name: filepath.Base(rel),
			Line: 1, End: min(len(lines), 40),
			Label: "file " + filepath.Base(rel),
		})
	}
	return entries, nil
}

func sliceLines(lines []string, start, end int) string {
	if start < 1 {
		start = 1
	}
	if end < start {
		end = start
	}
	if start > len(lines) {
		return ""
	}
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start-1:end], "\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func truncateSnippet(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
