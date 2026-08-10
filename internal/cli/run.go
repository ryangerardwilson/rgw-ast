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

	"github.com/ryangerardwilson/rgw-ast/internal/agents"
	"github.com/ryangerardwilson/rgw-ast/internal/config"
	"github.com/ryangerardwilson/rgw-ast/internal/doctor"
	"github.com/ryangerardwilson/rgw-ast/internal/enforce"
	"github.com/ryangerardwilson/rgw-ast/internal/execgen"
	"github.com/ryangerardwilson/rgw-ast/internal/files"
	"github.com/ryangerardwilson/rgw-ast/internal/hook"
	"github.com/ryangerardwilson/rgw-ast/internal/intel"
	"github.com/ryangerardwilson/rgw-ast/internal/measure"
	"github.com/ryangerardwilson/rgw-ast/internal/root"
	"github.com/ryangerardwilson/rgw-ast/internal/version"
)

const (
	ExitOK    = 0
	ExitFail  = 1
	ExitUsage = 2
)

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

type Runner struct {
	Out          io.Writer
	Err          io.Writer
	In           io.Reader
	Cwd          string
	ConfigPath   string
	LoadConfig   func() (config.Config, string, error)
	RootOverride string
	Refresh      bool
}

func NewRunner(out, errOut io.Writer) Runner {
	return Runner{Out: out, Err: errOut, In: os.Stdin}
}

func (r Runner) Run(args []string) int {
	args, rootFlag, err := stripRootFlag(args)
	if err != nil {
		return r.usage(err.Error())
	}
	if rootFlag != "" {
		r.RootOverride = rootFlag
	}
	if len(args) == 0 {
		WriteHelp(r.Out)
		return ExitOK
	}
	cmd := args[0]
	rest := args[1:]
	rest, rootFlag2, err := stripRootFlag(rest)
	if err != nil {
		return r.usage(err.Error())
	}
	if rootFlag2 != "" {
		r.RootOverride = rootFlag2
	}
	// global --refresh before/after verb for status/measure
	rest, refresh, err := stripBoolFlag(rest, "--refresh")
	if err != nil {
		return r.usage(err.Error())
	}
	r.Refresh = refresh

	switch cmd {
	case "help", "-h", "--help":
		if len(rest) != 0 {
			return r.usage("Use help by itself.")
		}
		WriteHelp(r.Out)
		return ExitOK
	case "version":
		err = r.cmdVersion(rest)
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
	case "explain":
		err = r.cmdExplain(rest)
	case "hash":
		err = r.cmdHash(rest)
	case "read":
		err = r.cmdRead(rest)
	case "create":
		err = r.cmdCreate(rest)
	case "append":
		err = r.cmdAppend(rest)
	case "patch":
		err = r.cmdPatch(rest)
	case "delete":
		err = r.cmdDelete(rest)
	case "exec":
		err = r.cmdExec(rest)
	case "doctor":
		err = r.cmdDoctor(rest)
	case "agents-block":
		err = r.cmdAgentsBlock(rest)
	case "hook":
		err = r.cmdHook(rest)
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

func stripRootFlag(args []string) (rest []string, root string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--root" {
			if i+1 >= len(args) {
				return nil, "", fmt.Errorf("--root requires a path")
			}
			if root != "" {
				return nil, "", fmt.Errorf("duplicate --root")
			}
			root = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(a, "--root=") {
			if root != "" {
				return nil, "", fmt.Errorf("duplicate --root")
			}
			root = strings.TrimPrefix(a, "--root=")
			continue
		}
		rest = append(rest, a)
	}
	return rest, root, nil
}

func stripBoolFlag(args []string, name string) ([]string, bool, error) {
	found := false
	var rest []string
	for _, a := range args {
		if a == name {
			if found {
				return nil, false, fmt.Errorf("duplicate %s", name)
			}
			found = true
			continue
		}
		rest = append(rest, a)
	}
	return rest, found, nil
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
	if r.RootOverride != "" {
		wsRoot, err = filepath.Abs(r.RootOverride)
		if err != nil {
			return
		}
		st, statErr := os.Stat(wsRoot)
		if statErr != nil {
			err = statErr
			return
		}
		if !st.IsDir() {
			err = fmt.Errorf("--root is not a directory: %s", wsRoot)
			return
		}
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

func (r Runner) measureRoot(cfg config.Config, wsRoot string) (measure.Result, error) {
	return measure.CountCachedOpts(wsRoot, cfg, measure.CountOpts{Refresh: r.Refresh})
}

// measureFresh forces a recount for mutation/hook-adjacent enforcement.
func (r Runner) measureFresh(cfg config.Config, wsRoot string) (measure.Result, error) {
	return measure.CountCachedOpts(wsRoot, cfg, measure.CountOpts{Refresh: true})
}

func (r Runner) cmdVersion(args []string) error {
	asJSON, rest, err := takeJSONFlag(args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return usageError{"Use: rgw-ast version [--json]"}
	}
	if asJSON {
		return writeJSON(r.Out, map[string]string{
			"version":    version.Version,
			"commit":     version.Commit,
			"build_time": version.BuildTime,
			"string":     version.String(),
		})
	}
	_, _ = fmt.Fprintln(r.Out, version.String())
	return nil
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
	Root               string `json:"root"`
	LOC                int    `json:"loc"`
	FileCount          int    `json:"file_count"`
	ThresholdLOC       int    `json:"threshold_loc"`
	Mode               string `json:"mode"`
	Enforced           bool   `json:"enforced"`
	Config             string `json:"config"`
	CacheHit           bool   `json:"cache_hit"`
	CacheAgeMs         int64  `json:"cache_age_ms"`
	NestedReposSkipped int    `json:"nested_repos_skipped"`
	GitIgnoreActive    bool   `json:"gitignore_active"`
}

func (r Runner) cmdStatus(args []string) error {
	asJSON, rest, err := takeJSONFlag(args)
	if err != nil {
		return err
	}
	rest, refresh, err := stripBoolFlag(rest, "--refresh")
	if err != nil {
		return err
	}
	if refresh {
		r.Refresh = true
	}
	if len(rest) != 0 {
		return usageError{"Use: rgw-ast status [--json] [--refresh]"}
	}
	cfg, cfgPath, wsRoot, err := r.workspace()
	if err != nil {
		return err
	}
	m, err := r.measureRoot(cfg, wsRoot)
	if err != nil {
		return err
	}
	d := enforce.Decide(cfg, m.LOC)
	out := statusOut{
		Root: m.Root, LOC: m.LOC, FileCount: m.FileCount,
		ThresholdLOC: d.ThresholdLOC, Mode: d.Mode, Enforced: d.Enforced,
		Config: cfgPath, CacheHit: m.CacheHit, CacheAgeMs: m.CacheAgeMs,
		NestedReposSkipped: m.NestedReposSkipped, GitIgnoreActive: m.GitIgnoreActive,
	}
	if asJSON {
		return writeJSON(r.Out, out)
	}
	_, _ = fmt.Fprintf(r.Out, "root: %s\nloc: %d\nfile_count: %d\nthreshold_loc: %d\nmode: %s\nenforced: %v\nconfig: %s\ncache_hit: %v\ncache_age_ms: %d\nnested_repos_skipped: %d\ngitignore_active: %v\n",
		out.Root, out.LOC, out.FileCount, out.ThresholdLOC, out.Mode, out.Enforced, out.Config, out.CacheHit, out.CacheAgeMs, out.NestedReposSkipped, out.GitIgnoreActive)
	return nil
}

func (r Runner) cmdMeasure(args []string) error {
	asJSON, rest, err := takeJSONFlag(args)
	if err != nil {
		return err
	}
	rest, refresh, err := stripBoolFlag(rest, "--refresh")
	if err != nil {
		return err
	}
	if refresh {
		r.Refresh = true
	}
	if len(rest) != 0 {
		return usageError{"Use: rgw-ast measure [--json] [--refresh]"}
	}
	cfg, _, wsRoot, err := r.workspace()
	if err != nil {
		return err
	}
	m, err := r.measureRoot(cfg, wsRoot)
	if err != nil {
		return err
	}
	if asJSON {
		return writeJSON(r.Out, map[string]any{
			"root": m.Root, "loc": m.LOC, "file_count": m.FileCount,
			"cache_hit": m.CacheHit, "nested_repos_skipped": m.NestedReposSkipped,
			"gitignore_active": m.GitIgnoreActive,
		})
	}
	_, _ = fmt.Fprintf(r.Out, "root: %s\nloc: %d\nfile_count: %d\n", m.Root, m.LOC, m.FileCount)
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
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		WriteSearchHelp(r.Out)
		return nil
	}
	pathFilter, glob, queryParts, err := parseSearchArgs(args)
	if err != nil {
		return err
	}
	if len(queryParts) == 0 {
		return usageError{"Use: rgw-ast search [--path dir] [--glob pat] <query>"}
	}
	query := strings.Join(queryParts, " ")
	if strings.HasPrefix(query, "-") {
		return usageError{"query looks like a flag; see rgw-ast search --help"}
	}
	cfg, _, wsRoot, err := r.workspace()
	if err != nil {
		return err
	}
	hits, truncated, err := intel.SearchOptsRun(wsRoot, query, cfg, intel.SearchOpts{
		PathPrefix: pathFilter,
		Glob:       glob,
	})
	if err != nil {
		return err
	}
	for _, h := range hits {
		_, _ = fmt.Fprintf(r.Out, "%s:%d:%s\n", h.Path, h.Line, h.Content)
	}
	if truncated {
		_, _ = fmt.Fprintf(r.Err, "truncated at %d hits\n", cfg.Enforcement.MaxSearchHits)
	}
	if len(hits) == 0 {
		_, _ = fmt.Fprintf(r.Err, "no hits (check include globs, --path, nested-git skips; try rgw-ast explain <path>)\n")
	}
	return nil
}

func parseSearchArgs(args []string) (path, glob string, query []string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--path":
			if i+1 >= len(args) {
				return "", "", nil, usageError{"--path requires a value"}
			}
			path = args[i+1]
			i++
		case strings.HasPrefix(a, "--path="):
			path = strings.TrimPrefix(a, "--path=")
		case a == "--glob":
			if i+1 >= len(args) {
				return "", "", nil, usageError{"--glob requires a value"}
			}
			glob = args[i+1]
			i++
		case strings.HasPrefix(a, "--glob="):
			glob = strings.TrimPrefix(a, "--glob=")
		case a == "--help" || a == "-h":
			return "", "", nil, usageError{"use: rgw-ast search --help"}
		case strings.HasPrefix(a, "-"):
			return "", "", nil, usageError{fmt.Sprintf("unknown search flag %q", a)}
		default:
			query = append(query, a)
		}
	}
	return path, glob, query, nil
}

func (r Runner) cmdExplain(args []string) error {
	if len(args) != 1 {
		return usageError{"Use: rgw-ast explain <path>"}
	}
	cfg, _, wsRoot, err := r.workspace()
	if err != nil {
		return err
	}
	out, err := intel.Explain(wsRoot, args[0], cfg)
	if err != nil {
		return err
	}
	_, _ = io.WriteString(r.Out, out)
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
	path, linesSpec, number, strict, err := parseReadArgs(args)
	if err != nil {
		return err
	}
	cfg, _, wsRoot, err := r.workspace()
	if err != nil {
		return err
	}
	m, err := r.measureFresh(cfg, wsRoot)
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
		if d.Enforced && cfg.Enforcement.DenyWholeFileRead {
			bin, err := files.IsBinary(abs)
			if err != nil {
				return err
			}
			if !bin {
				return fmt.Errorf("whole-file read denied while enforced; use --lines START-END or map/show/search")
			}
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
	text, next, err := files.FormatReadRange(abs, rel, start, end, cfg.Enforcement.MaxReadLines, number, strict)
	if err != nil {
		return err
	}
	_, _ = io.WriteString(r.Out, text)
	if next > 0 {
		_, _ = fmt.Fprintf(r.Err, "next_start: %d\n", next)
	}
	return nil
}

func (r Runner) cmdCreate(args []string) error {
	path, fromFile, stdin, parents, expectAbsent, err := parseCreateArgs(args)
	if err != nil {
		return err
	}
	if !expectAbsent {
		return usageError{"--expect-absent is required"}
	}
	if fromFile == "" && !stdin {
		return usageError{"provide --from-file or --stdin"}
	}
	_, _, wsRoot, err := r.workspace()
	if err != nil {
		return err
	}
	abs, err := files.ResolvePath(wsRoot, path)
	if err != nil {
		return err
	}
	var content []byte
	if stdin {
		content, err = files.ReadStdin(r.In)
	} else {
		content, err = files.ReadAllFile(fromFile)
	}
	if err != nil {
		return err
	}
	h, err := files.CreateAbsent(abs, content, parents, 0o644)
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(wsRoot, abs)
	_, _ = fmt.Fprintf(r.Out, "ok  %s  %s\n", h, filepath.ToSlash(rel))
	return nil
}

func (r Runner) cmdAppend(args []string) error {
	path, hash, fromFile, stdin, err := parseAppendArgs(args)
	if err != nil {
		return err
	}
	if hash == "" {
		return usageError{"--expect-hash is required"}
	}
	_, _, wsRoot, err := r.workspace()
	if err != nil {
		return err
	}
	abs, err := files.ResolvePath(wsRoot, path)
	if err != nil {
		return err
	}
	var content []byte
	if stdin {
		content, err = files.ReadStdin(r.In)
	} else {
		content, err = files.ReadAllFile(fromFile)
	}
	if err != nil {
		return err
	}
	h, err := files.AppendExact(abs, hash, content)
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(wsRoot, abs)
	_, _ = fmt.Fprintf(r.Out, "ok  %s  %s\n", h, filepath.ToSlash(rel))
	return nil
}

func (r Runner) cmdPatch(args []string) error {
	path, expectHash, old, newText, oldFile, newFile, opsFile, err := parsePatchArgs(args)
	if err != nil {
		return err
	}
	cfg, _, wsRoot, err := r.workspace()
	if err != nil {
		return err
	}
	// recheck enforcement with fresh measure (mutations must not use stale LOC)
	m, err := r.measureFresh(cfg, wsRoot)
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
	var ops []files.Op
	switch {
	case opsFile != "":
		ops, err = files.ParseOpsFile(opsFile)
		if err != nil {
			return err
		}
	case oldFile != "" || newFile != "":
		if oldFile == "" || newFile == "" {
			return usageError{"both --old-file and --new-file are required together"}
		}
		ob, err := files.ReadAllFile(oldFile)
		if err != nil {
			return err
		}
		nb, err := files.ReadAllFile(newFile)
		if err != nil {
			return err
		}
		ops = []files.Op{{Old: string(ob), New: string(nb)}}
	default:
		if old == "" {
			return usageError{"--old or --old-file or --ops-file is required"}
		}
		ops = []files.Op{{Old: old, New: newText}}
	}
	newHash, err := files.PatchOps(abs, expectHash, ops)
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(wsRoot, abs)
	_, _ = fmt.Fprintf(r.Out, "ok  %s  %s\n", newHash, filepath.ToSlash(rel))
	return nil
}

func (r Runner) cmdDelete(args []string) error {
	path, expectHash, pruneEmpty, err := parseDeleteArgs(args)
	if err != nil {
		return err
	}
	if expectHash == "" {
		return usageError{"--expect-hash is required"}
	}
	cfg, _, wsRoot, err := r.workspace()
	if err != nil {
		return err
	}
	if _, err := r.measureFresh(cfg, wsRoot); err != nil {
		return err
	}
	abs, err := files.ResolvePath(wsRoot, path)
	if err != nil {
		return err
	}
	pruned, err := files.DeleteExact(wsRoot, abs, expectHash, pruneEmpty)
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(wsRoot, abs)
	_, _ = fmt.Fprintf(r.Out, "ok  deleted  %s\n", filepath.ToSlash(rel))
	for _, dir := range pruned {
		rel, _ := filepath.Rel(wsRoot, dir)
		_, _ = fmt.Fprintf(r.Out, "ok  pruned  %s\n", filepath.ToSlash(rel))
	}
	return nil
}

func (r Runner) cmdHook(args []string) error {
	if len(args) != 0 {
		return usageError{"Use: rgw-ast hook"}
	}
	cfg, _, err := r.load()
	if err != nil {
		_ = json.NewEncoder(r.Out).Encode(map[string]any{
			"permissionDecision": "deny", "permissionDecisionReason": err.Error(),
		})
		return nil
	}
	cwd := r.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	in := r.In
	if in == nil {
		in = os.Stdin
	}
	if err := hook.Run(in, r.Out, cfg, cwd, r.RootOverride); err != nil {
		_ = json.NewEncoder(r.Out).Encode(map[string]any{
			"permissionDecision": "deny", "permissionDecisionReason": err.Error(),
		})
	}
	return nil
}

func (r Runner) cmdExec(args []string) error {
	// rgw-ast exec [--json] -- <command...>
	asJSON := false
	var cmdArgs []string
	seenSep := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			seenSep = true
			cmdArgs = args[i+1:]
			break
		}
		if a == "--json" {
			asJSON = true
			continue
		}
		return usageError{"Use: rgw-ast exec [--json] -- <command...>"}
	}
	if !seenSep || len(cmdArgs) == 0 {
		return usageError{"Use: rgw-ast exec [--json] -- <command...>"}
	}
	cfg, _, wsRoot, err := r.workspace()
	if err != nil {
		return err
	}
	m, err := r.measureFresh(cfg, wsRoot)
	if err != nil {
		return err
	}
	d := enforce.Decide(cfg, m.LOC)
	rep, err := execgen.Run(wsRoot, cfg, cmdArgs, d.Enforced)
	if err != nil {
		return err
	}
	if asJSON {
		if err := writeJSON(r.Out, rep); err != nil {
			return err
		}
	} else {
		_, _ = fmt.Fprintf(r.Out, "exit_code: %d\nallowed_by: %s\nchanges: %d\n", rep.ExitCode, rep.AllowedBy, len(rep.Changes))
		for _, c := range rep.Changes {
			if c.Hash != "" {
				_, _ = fmt.Fprintf(r.Out, "  %s  %s  %s\n", c.Status, c.Hash, c.Path)
			} else {
				_, _ = fmt.Fprintf(r.Out, "  %s  %s\n", c.Status, c.Path)
			}
		}
	}
	if rep.ExitCode != 0 {
		return fmt.Errorf("generator exit %d", rep.ExitCode)
	}
	return nil
}

func (r Runner) cmdDoctor(args []string) error {
	asJSON, rest, err := takeJSONFlag(args)
	if err != nil {
		return err
	}
	rest, refresh, err := stripBoolFlag(rest, "--refresh")
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return usageError{"Use: rgw-ast doctor [--json] [--refresh]"}
	}
	cfg, cfgPath, err := r.load()
	if err != nil {
		return err
	}
	cwd := r.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	rep := doctor.Run(cfg, cfgPath, cwd, r.RootOverride, refresh || r.Refresh)
	if asJSON {
		b, err := doctor.FormatJSON(rep)
		if err != nil {
			return err
		}
		_, _ = r.Out.Write(b)
		_, _ = fmt.Fprintln(r.Out)
		return nil
	}
	_, _ = io.WriteString(r.Out, doctor.FormatText(rep))
	if !rep.OK {
		return fmt.Errorf("doctor reported errors")
	}
	return nil
}

func (r Runner) cmdAgentsBlock(args []string) error {
	if len(args) != 0 {
		return usageError{"Use: rgw-ast agents-block"}
	}
	_, _ = io.WriteString(r.Out, agents.Block)
	if !strings.HasSuffix(agents.Block, "\n") {
		_, _ = fmt.Fprintln(r.Out)
	}
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
		rest = append(rest, a)
	}
	return asJSON, rest, nil
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func parseReadArgs(args []string) (path, lines string, number, strict bool, err error) {
	if len(args) == 0 {
		return "", "", false, false, usageError{"Use: rgw-ast read <file> --lines <start>-<end>"}
	}
	path = args[0]
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--lines":
			if i+1 >= len(args) {
				return "", "", false, false, usageError{"--lines requires START-END"}
			}
			lines = args[i+1]
			i++
		case strings.HasPrefix(a, "--lines="):
			lines = strings.TrimPrefix(a, "--lines=")
		case a == "--number" || a == "-n":
			number = true
		case a == "--strict-lines":
			strict = true
		default:
			return "", "", false, false, usageError{fmt.Sprintf("unexpected argument %q", a)}
		}
	}
	return path, lines, number, strict, nil
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

func parseCreateArgs(args []string) (path, fromFile string, stdin, parents, expectAbsent bool, err error) {
	if len(args) < 1 {
		return "", "", false, false, false, usageError{"Use: rgw-ast create <file> --expect-absent ..."}
	}
	path = args[0]
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--expect-absent":
			expectAbsent = true
		case a == "--parents":
			parents = true
		case a == "--stdin":
			stdin = true
		case a == "--from-file":
			if i+1 >= len(args) {
				return "", "", false, false, false, usageError{"--from-file requires a path"}
			}
			fromFile = args[i+1]
			i++
		case strings.HasPrefix(a, "--from-file="):
			fromFile = strings.TrimPrefix(a, "--from-file=")
		default:
			return "", "", false, false, false, usageError{fmt.Sprintf("unexpected %q", a)}
		}
	}
	return path, fromFile, stdin, parents, expectAbsent, nil
}

func parseAppendArgs(args []string) (path, hash, fromFile string, stdin bool, err error) {
	if len(args) < 1 {
		return "", "", "", false, usageError{"Use: rgw-ast append <file> --expect-hash <sha> ..."}
	}
	path = args[0]
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--expect-hash":
			if i+1 >= len(args) {
				return "", "", "", false, usageError{"--expect-hash requires a value"}
			}
			hash = args[i+1]
			i++
		case strings.HasPrefix(a, "--expect-hash="):
			hash = strings.TrimPrefix(a, "--expect-hash=")
		case a == "--stdin":
			stdin = true
		case a == "--from-file":
			if i+1 >= len(args) {
				return "", "", "", false, usageError{"--from-file requires a path"}
			}
			fromFile = args[i+1]
			i++
		case strings.HasPrefix(a, "--from-file="):
			fromFile = strings.TrimPrefix(a, "--from-file=")
		default:
			return "", "", "", false, usageError{fmt.Sprintf("unexpected %q", a)}
		}
	}
	if fromFile == "" && !stdin {
		return "", "", "", false, usageError{"provide --from-file or --stdin"}
	}
	return path, hash, fromFile, stdin, nil
}

func parseDeleteArgs(args []string) (path, hash string, pruneEmpty bool, err error) {
	if len(args) < 1 {
		return "", "", false, usageError{"Use: rgw-ast delete <file> --expect-hash <sha> [--prune-empty]"}
	}
	path = args[0]
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--expect-hash":
			if i+1 >= len(args) {
				return "", "", false, usageError{"--expect-hash requires a value"}
			}
			hash = args[i+1]
			i++
		case strings.HasPrefix(a, "--expect-hash="):
			hash = strings.TrimPrefix(a, "--expect-hash=")
		case a == "--prune-empty":
			pruneEmpty = true
		default:
			return "", "", false, usageError{fmt.Sprintf("unexpected argument %q", a)}
		}
	}
	return path, hash, pruneEmpty, nil
}

func parsePatchArgs(args []string) (path, hash, old, new, oldFile, newFile, opsFile string, err error) {
	if len(args) < 1 {
		return "", "", "", "", "", "", "", usageError{"Use: rgw-ast patch <file> --expect-hash <sha> ..."}
	}
	path = args[0]
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--expect-hash":
			if i+1 >= len(args) {
				return "", "", "", "", "", "", "", usageError{"--expect-hash requires a value"}
			}
			hash = args[i+1]
			i++
		case strings.HasPrefix(a, "--expect-hash="):
			hash = strings.TrimPrefix(a, "--expect-hash=")
		case a == "--old":
			if i+1 >= len(args) {
				return "", "", "", "", "", "", "", usageError{"--old requires a value"}
			}
			old = args[i+1]
			i++
		case strings.HasPrefix(a, "--old="):
			old = strings.TrimPrefix(a, "--old=")
		case a == "--new":
			if i+1 >= len(args) {
				return "", "", "", "", "", "", "", usageError{"--new requires a value"}
			}
			new = args[i+1]
			i++
		case strings.HasPrefix(a, "--new="):
			new = strings.TrimPrefix(a, "--new=")
		case a == "--old-file":
			if i+1 >= len(args) {
				return "", "", "", "", "", "", "", usageError{"--old-file requires a path"}
			}
			oldFile = args[i+1]
			i++
		case strings.HasPrefix(a, "--old-file="):
			oldFile = strings.TrimPrefix(a, "--old-file=")
		case a == "--new-file":
			if i+1 >= len(args) {
				return "", "", "", "", "", "", "", usageError{"--new-file requires a path"}
			}
			newFile = args[i+1]
			i++
		case strings.HasPrefix(a, "--new-file="):
			newFile = strings.TrimPrefix(a, "--new-file=")
		case a == "--ops-file":
			if i+1 >= len(args) {
				return "", "", "", "", "", "", "", usageError{"--ops-file requires a path"}
			}
			opsFile = args[i+1]
			i++
		case strings.HasPrefix(a, "--ops-file="):
			opsFile = strings.TrimPrefix(a, "--ops-file=")
		default:
			return "", "", "", "", "", "", "", usageError{fmt.Sprintf("unexpected argument %q", a)}
		}
	}
	return path, hash, old, new, oldFile, newFile, opsFile, nil
}
