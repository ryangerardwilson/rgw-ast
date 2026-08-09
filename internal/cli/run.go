package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ryangerardwilson/rgw-ast/internal/config"
	"github.com/ryangerardwilson/rgw-ast/internal/enforce"
	"github.com/ryangerardwilson/rgw-ast/internal/files"
	"github.com/ryangerardwilson/rgw-ast/internal/intel"
	"github.com/ryangerardwilson/rgw-ast/internal/measure"
	"github.com/ryangerardwilson/rgw-ast/internal/root"
	"github.com/ryangerardwilson/rgw-ast/internal/version"
)

// Exit codes per spec.
const (
	ExitOK      = 0
	ExitFail    = 1
	ExitUsage   = 2
)

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

// Runner is the CLI entry.
type Runner struct {
	Out io.Writer
	Err io.Writer
	// Cwd overrides process cwd for tests.
	Cwd string
	// ConfigPath overrides global config path for tests.
	ConfigPath string
	// LoadConfig if set replaces default load (tests).
	LoadConfig func() (config.Config, string, error)
}

func NewRunner(out, errOut io.Writer) Runner {
	return Runner{Out: out, Err: errOut}
}

func (r Runner) Run(args []string) int {
	if len(args) == 0 {
		WriteHelp(r.Out)
		return ExitOK
	}
	cmd := args[0]
	rest := args[1:]
	var err error
	switch cmd {
	case "help", "-h", "--help":
		if len(rest) != 0 {
			return r.usage("Use help by itself.")
		}
		WriteHelp(r.Out)
		return ExitOK
	case "version":
		if len(rest) != 0 {
			return r.usage("Use version by itself.")
		}
		_, _ = fmt.Fprintln(r.Out, version.Version)
		return ExitOK
	case "config":
		err = r.cmdConfig(rest)
	case "status":
		err = r.cmdStatus(rest)
	case "measure":
		err = r.cmdMeasure(rest)
	case "map":
		err = r.cmdMap(rest)
	case "show":
		err = r.cmdShow(rest)
	case "search":
		err = r.cmdSearch(rest)
	case "hash":
		err = r.cmdHash(rest)
	case "read":
		err = r.cmdRead(rest)
	case "patch":
		err = r.cmdPatch(rest)
	default:
		return r.usage(fmt.Sprintf("unknown command %q; see rgw-ast help", cmd))
	}
	if err == nil {
		return ExitOK
	}
	var ue usageError
	if errors.As(err, &ue) {
		_, _ = fmt.Fprintln(r.Err, "Error:", ue.Error())
		return ExitUsage
	}
	_, _ = fmt.Fprintln(r.Err, "Error:", err.Error())
	return ExitFail
}

func (r Runner) usage(msg string) int {
	_, _ = fmt.Fprintln(r.Err, "Error:", msg)
	return ExitUsage
}

func (r Runner) load() (config.Config, string, error) {
	if r.LoadConfig != nil {
		return r.LoadConfig()
	}
	if r.ConfigPath != "" {
		if err := config.Ensure(r.ConfigPath); err != nil {
			return config.Config{}, "", err
		}
		cfg, err := config.Read(r.ConfigPath)
		return cfg, r.ConfigPath, err
	}
	return config.Load()
}

func (r Runner) workspace() (cfg config.Config, cfgPath, wsRoot string, err error) {
	cfg, cfgPath, err = r.load()
	if err != nil {
		return
	}
	cwd := r.Cwd
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return
		}
	}
	wsRoot, err = root.Resolve(cwd)
	return
}

func (r Runner) cmdConfig(args []string) error {
	if len(args) != 0 {
		return usageError{"Use: rgw-ast config"}
	}
	_, path, err := r.load()
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(r.Out, abs)
	return nil
}

type statusOut struct {
	Root         string `json:"root"`
	LOC          int    `json:"loc"`
	FileCount    int    `json:"file_count"`
	ThresholdLOC int    `json:"threshold_loc"`
	Mode         string `json:"mode"`
	Enforced     bool   `json:"enforced"`
	Config       string `json:"config"`
}

func (r Runner) cmdStatus(args []string) error {
	asJSON, rest, err := takeJSONFlag(args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return usageError{"Use: rgw-ast status [--json]"}
	}
	cfg, cfgPath, wsRoot, err := r.workspace()
	if err != nil {
		return err
	}
	m, err := measure.Count(wsRoot, cfg)
	if err != nil {
		return err
	}
	d := enforce.Decide(cfg, m.LOC)
	out := statusOut{
		Root:         m.Root,
		LOC:          m.LOC,
		FileCount:    m.FileCount,
		ThresholdLOC: d.ThresholdLOC,
		Mode:         d.Mode,
		Enforced:     d.Enforced,
		Config:       cfgPath,
	}
	if asJSON {
		return writeJSON(r.Out, out)
	}
	_, _ = fmt.Fprintf(r.Out, "root: %s\n", out.Root)
	_, _ = fmt.Fprintf(r.Out, "loc: %d\n", out.LOC)
	_, _ = fmt.Fprintf(r.Out, "file_count: %d\n", out.FileCount)
	_, _ = fmt.Fprintf(r.Out, "threshold_loc: %d\n", out.ThresholdLOC)
	_, _ = fmt.Fprintf(r.Out, "mode: %s\n", out.Mode)
	_, _ = fmt.Fprintf(r.Out, "enforced: %v\n", out.Enforced)
	_, _ = fmt.Fprintf(r.Out, "config: %s\n", out.Config)
	return nil
}

func (r Runner) cmdMeasure(args []string) error {
	asJSON, rest, err := takeJSONFlag(args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return usageError{"Use: rgw-ast measure [--json]"}
	}
	cfg, _, wsRoot, err := r.workspace()
	if err != nil {
		return err
	}
	m, err := measure.Count(wsRoot, cfg)
	if err != nil {
		return err
	}
	if asJSON {
		return writeJSON(r.Out, map[string]any{
			"root":       m.Root,
			"loc":        m.LOC,
			"file_count": m.FileCount,
		})
	}
	_, _ = fmt.Fprintf(r.Out, "root: %s\n", m.Root)
	_, _ = fmt.Fprintf(r.Out, "loc: %d\n", m.LOC)
	_, _ = fmt.Fprintf(r.Out, "file_count: %d\n", m.FileCount)
	return nil
}

func (r Runner) cmdMap(args []string) error {
	path := ""
	if len(args) > 1 {
		return usageError{"Use: rgw-ast map [path]"}
	}
	if len(args) == 1 {
		path = args[0]
	}
	cfg, _, wsRoot, err := r.workspace()
	if err != nil {
		return err
	}
	entries, err := intel.Map(wsRoot, path, cfg, cfg.MaxMapEntries)
	if err != nil {
		return err
	}
	for _, e := range entries {
		_, _ = fmt.Fprintf(r.Out, "%s:%d %s\n", e.Path, e.Line, e.Label)
	}
	return nil
}

func (r Runner) cmdShow(args []string) error {
	if len(args) != 1 {
		return usageError{"Use: rgw-ast show <symbol|path:symbol>"}
	}
	cfg, _, wsRoot, err := r.workspace()
	if err != nil {
		return err
	}
	body, err := intel.Show(wsRoot, args[0], cfg)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(r.Out, body)
	return nil
}

func (r Runner) cmdSearch(args []string) error {
	if len(args) < 1 {
		return usageError{"Use: rgw-ast search <query>"}
	}
	query := strings.Join(args, " ")
	cfg, _, wsRoot, err := r.workspace()
	if err != nil {
		return err
	}
	hits, truncated, err := intel.Search(wsRoot, query, cfg)
	if err != nil {
		return err
	}
	for _, h := range hits {
		_, _ = fmt.Fprintf(r.Out, "%s:%d:%s\n", h.Path, h.Line, h.Content)
	}
	if truncated {
		_, _ = fmt.Fprintf(r.Err, "truncated at %d hits\n", cfg.Enforcement.MaxSearchHits)
	}
	return nil
}

func (r Runner) cmdHash(args []string) error {
	if len(args) < 1 {
		return usageError{"Use: rgw-ast hash <file> [<file>...]"}
	}
	_, _, wsRoot, err := r.workspace()
	if err != nil {
		return err
	}
	for _, p := range args {
		abs, err := files.ResolvePath(wsRoot, p)
		if err != nil {
			return err
		}
		h, err := files.HashFile(abs)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(wsRoot, abs)
		if err != nil {
			rel = abs
		}
		_, _ = fmt.Fprintf(r.Out, "%s  %s\n", h, filepath.ToSlash(rel))
	}
	return nil
}

func (r Runner) cmdRead(args []string) error {
	path, linesSpec, err := parseReadArgs(args)
	if err != nil {
		return err
	}
	cfg, _, wsRoot, err := r.workspace()
	if err != nil {
		return err
	}
	m, err := measure.Count(wsRoot, cfg)
	if err != nil {
		return err
	}
	d := enforce.Decide(cfg, m.LOC)
	abs, err := files.ResolvePath(wsRoot, path)
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(wsRoot, abs)
	rel = filepath.ToSlash(rel)

	if linesSpec == "" {
		if d.Enforced && cfg.Enforcement.DenyWholeFileRead && measure.PathMatches(cfg, rel) {
			return fmt.Errorf("whole-file read denied while enforced; use --lines START-END or map/show/search")
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return err
		}
		_, _ = r.Out.Write(data)
		if len(data) > 0 && data[len(data)-1] != '\n' {
			_, _ = fmt.Fprintln(r.Out)
		}
		return nil
	}
	start, end, err := parseLineRange(linesSpec)
	if err != nil {
		return usageError{err.Error()}
	}
	span := end - start + 1
	if span > cfg.Enforcement.MaxReadLines {
		return fmt.Errorf("line range length %d exceeds max_read_lines %d", span, cfg.Enforcement.MaxReadLines)
	}
	text, err := files.ReadLines(abs, start, end)
	if err != nil {
		return err
	}
	_, _ = io.WriteString(r.Out, text)
	if text != "" && !strings.HasSuffix(text, "\n") {
		_, _ = fmt.Fprintln(r.Out)
	}
	return nil
}

func (r Runner) cmdPatch(args []string) error {
	path, expectHash, old, newText, err := parsePatchArgs(args)
	if err != nil {
		return err
	}
	cfg, _, wsRoot, err := r.workspace()
	if err != nil {
		return err
	}
	m, err := measure.Count(wsRoot, cfg)
	if err != nil {
		return err
	}
	d := enforce.Decide(cfg, m.LOC)
	if d.Enforced && cfg.Enforcement.RequireHashBeforePatch && expectHash == "" {
		return usageError{"--expect-hash is required while enforced"}
	}
	if expectHash == "" {
		return usageError{"--expect-hash is required"}
	}
	abs, err := files.ResolvePath(wsRoot, path)
	if err != nil {
		return err
	}
	newHash, err := files.PatchExact(abs, expectHash, old, newText)
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(wsRoot, abs)
	_, _ = fmt.Fprintf(r.Out, "ok  %s  %s\n", newHash, filepath.ToSlash(rel))
	return nil
}

func takeJSONFlag(args []string) (bool, []string, error) {
	asJSON := false
	var rest []string
	for _, a := range args {
		if a == "--json" {
			if asJSON {
				return false, nil, usageError{"duplicate --json"}
			}
			asJSON = true
			continue
		}
		if strings.HasPrefix(a, "-") {
			return false, nil, usageError{fmt.Sprintf("unknown flag %q", a)}
		}
		rest = append(rest, a)
	}
	return asJSON, rest, nil
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func parseReadArgs(args []string) (path, lines string, err error) {
	if len(args) == 0 {
		return "", "", usageError{"Use: rgw-ast read <file> --lines <start>-<end>"}
	}
	path = args[0]
	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "--lines" {
			if i+1 >= len(args) {
				return "", "", usageError{"--lines requires START-END"}
			}
			lines = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(a, "--lines=") {
			lines = strings.TrimPrefix(a, "--lines=")
			continue
		}
		return "", "", usageError{fmt.Sprintf("unexpected argument %q", a)}
	}
	return path, lines, nil
}

func parseLineRange(s string) (start, end int, err error) {
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("lines must be START-END")
	}
	start, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid start line")
	}
	end, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid end line")
	}
	if start < 1 || end < start {
		return 0, 0, fmt.Errorf("invalid line range %s", s)
	}
	return start, end, nil
}

func parsePatchArgs(args []string) (path, hash, old, new string, err error) {
	if len(args) < 1 {
		return "", "", "", "", usageError{"Use: rgw-ast patch <file> --expect-hash <sha> --old <text> --new <text>"}
	}
	path = args[0]
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--expect-hash":
			if i+1 >= len(args) {
				return "", "", "", "", usageError{"--expect-hash requires a value"}
			}
			hash = args[i+1]
			i++
		case strings.HasPrefix(a, "--expect-hash="):
			hash = strings.TrimPrefix(a, "--expect-hash=")
		case a == "--old":
			if i+1 >= len(args) {
				return "", "", "", "", usageError{"--old requires a value"}
			}
			old = args[i+1]
			i++
		case strings.HasPrefix(a, "--old="):
			old = strings.TrimPrefix(a, "--old=")
		case a == "--new":
			if i+1 >= len(args) {
				return "", "", "", "", usageError{"--new requires a value"}
			}
			new = args[i+1]
			i++
		case strings.HasPrefix(a, "--new="):
			new = strings.TrimPrefix(a, "--new=")
		default:
			return "", "", "", "", usageError{fmt.Sprintf("unexpected argument %q", a)}
		}
	}
	if old == "" {
		return "", "", "", "", usageError{"--old is required"}
	}
	// --new may be empty (delete text)
	return path, hash, old, new, nil
}
