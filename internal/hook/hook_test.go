package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ryangerardwilson/rgw-ast/internal/config"
)

func TestHookDenyWriteWhenEnforced(t *testing.T) {
	root := t.TempDir()
	// enough lines to exceed threshold 1
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n//1\n//2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.ThresholdLOC = 1
	cfg.Enforcement.Mode = "auto"
	cfg.Cache.Dir = t.TempDir()

	in := bytes.NewBufferString(`{"tool_name":"Write","tool_input":{"path":"a.go"}}`)
	var out bytes.Buffer
	if err := Run(in, &out, cfg, root, root); err != nil {
		t.Fatal(err)
	}
	var dec Decision
	if err := json.Unmarshal(out.Bytes(), &dec); err != nil {
		t.Fatal(err, out.String())
	}
	if dec.PermissionDecision != "deny" {
		t.Fatalf("%+v", dec)
	}

	in2 := bytes.NewBufferString(`{"tool_name":"Bash","tool_input":{"command":"rgw-ast status"}}`)
	out.Reset()
	if err := Run(in2, &out, cfg, root, root); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out.Bytes(), &dec); err != nil {
		t.Fatal(err)
	}
	if dec.PermissionDecision != "allow" {
		t.Fatalf("%+v", dec)
	}
}
