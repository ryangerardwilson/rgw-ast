package files

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateAndBatchPatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "n.go")
	h, err := CreateAbsent(path, []byte("package n\nconst A=1\nconst B=2\n"), false, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateAbsent(path, []byte("x"), false, 0o644); err == nil {
		t.Fatal("expected exists error")
	}
	h2, err := PatchOps(path, h, []Op{
		{Old: "A=1", New: "A=10"},
		{Old: "B=2", New: "B=20"},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "package n\nconst A=10\nconst B=20\n" {
		t.Fatalf("%q", data)
	}
	if h2 == h {
		t.Fatal("hash should change")
	}
}
