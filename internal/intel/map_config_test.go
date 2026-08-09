package intel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryangerardwilson/rgw-ast/internal/config"
)

func TestMapMarkdownJSON(t *testing.T) {
	root := t.TempDir()
	md := filepath.Join(root, "s.md")
	if err := os.WriteFile(md, []byte("# Title\n\n### Requirement: Foo\n\n#### Scenario: Bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	js := filepath.Join(root, "c.json")
	if err := os.WriteFile(js, []byte(`{"a":{"b":1},"c":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	ents, err := Map(root, "s.md", cfg, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range ents {
		if e.Kind == "requirement" && strings.Contains(e.Name, "Foo") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing requirement: %+v", ents)
	}
	ents, err = Map(root, "c.json", cfg, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) == 0 {
		t.Fatal("expected json keys")
	}
}
