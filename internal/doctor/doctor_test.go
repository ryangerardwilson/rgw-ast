package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ryangerardwilson/rgw-ast/internal/config"
)

func TestDoctorAgentsMissing(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Cache.Dir = t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Write(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	rep := Run(cfg, cfgPath, root, root, true)
	if rep.AgentsBlockReady {
		t.Fatal("should not be ready without AGENTS.md")
	}
	if rep.AgentsBlockStatus != "absent_file" {
		t.Fatalf("%s", rep.AgentsBlockStatus)
	}
	if rep.HookReady {
		t.Fatal("hook should not be ready without evidence")
	}
	if rep.HookStatus != "not_checked" {
		t.Fatalf("%s", rep.HookStatus)
	}
}

func TestDoctorAgentsCurrent(t *testing.T) {
	root := t.TempDir()
	block := "<!-- rgw-ast:begin -->\nrgw-ast status --json\nrgw-ast exec\n<!-- rgw-ast:end -->\n"
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(block), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Cache.Dir = t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "c.toml")
	_ = config.Write(cfgPath, cfg)
	rep := Run(cfg, cfgPath, root, root, true)
	if !rep.AgentsBlockReady || rep.AgentsBlockStatus != "current" {
		t.Fatalf("%+v", rep)
	}
}
