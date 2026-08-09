package hook

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ryangerardwilson/rgw-ast/internal/config"
)

func TestHookDeniesMutationWithRgwComment(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n//1\n//2\n//3\n//4\n//5\n//6\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.ThresholdLOC = 1
	cfg.Enforcement.Mode = "auto"
	cfg.Cache.Dir = t.TempDir()

	in := bytes.NewBufferString(`{"tool_name":"Bash","tool_input":{"command":"printf x > should-be-denied.txt # rgw-ast"}}`)
	var out bytes.Buffer
	if err := Run(in, &out, cfg, root, root); err != nil {
		t.Fatal(err)
	}
	var dec Decision
	if err := json.Unmarshal(out.Bytes(), &dec); err != nil {
		t.Fatal(err, out.String())
	}
	if dec.PermissionDecision != "deny" {
		t.Fatalf("expected deny, got %s (%s)", dec.PermissionDecision, out.String())
	}
}

func TestHookAllowsLeadingRgwAst(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n//1\n//2\n//3\n//4\n//5\n//6\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.ThresholdLOC = 1
	cfg.Cache.Dir = t.TempDir()
	in := bytes.NewBufferString(`{"tool_name":"Bash","tool_input":{"command":"rgw-ast status --json"}}`)
	var out bytes.Buffer
	if err := Run(in, &out, cfg, root, root); err != nil {
		t.Fatal(err)
	}
	var dec Decision
	if err := json.Unmarshal(out.Bytes(), &dec); err != nil {
		t.Fatal(err)
	}
	if dec.PermissionDecision != "allow" {
		t.Fatalf("%s", out.String())
	}
}

func TestLeadingTokens(t *testing.T) {
	if !isRGWAstInvocation("rgw-ast exec -- npm exec -- openspec") {
		t.Fatal("direct")
	}
	if isRGWAstInvocation("printf x > f # rgw-ast") {
		t.Fatal("comment")
	}
	if isRGWAstInvocation("echo rgw-ast") {
		t.Fatal("arg only")
	}
}
