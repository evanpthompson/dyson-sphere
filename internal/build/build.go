// Package build carries identity that is stamped in at link time rather than
// hardcoded, so a running binary can say exactly which commit produced it.
package build

// These are overridden with -ldflags "-X gitlab.com/.../internal/build.Version=..."
// See the Makefile. The defaults are what you get from a plain `go build`.
var (
	ServiceName = "dyson-sphere-service"
	Version     = "dev"
	Commit      = "unknown"
)
