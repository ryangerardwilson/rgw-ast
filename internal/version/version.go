package version

// Version is the semantic version, stamped at build/release time.
// Source defaults are replaced via -ldflags when building releases or install.sh.
var Version = "0.0.0-dev"

// Commit is the short git SHA when available (ldflags).
var Commit = ""

// BuildTime is RFC3339 UTC build time when stamped (ldflags).
var BuildTime = ""

// String returns a human-readable version line.
func String() string {
	if Commit == "" {
		return Version
	}
	if Version == "0.0.0-dev" || Version == "0.0.0" {
		return Version + "+" + Commit
	}
	return Version + "+" + Commit
}
