package execgen

import (
	"testing"

	"github.com/ryangerardwilson/rgw-ast/internal/config"
)

func TestAllowed(t *testing.T) {
	cfg := config.Default()
	ok, _ := Allowed(cfg, "npm exec -- openspec archive foo --yes")
	if !ok {
		t.Fatal("openspec should be allowed")
	}
	ok, _ = Allowed(cfg, "rm -rf /")
	if ok {
		t.Fatal("rm should not be allowed")
	}
}
