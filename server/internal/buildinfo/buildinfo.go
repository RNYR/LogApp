// Package buildinfo carries the version metadata of the running binary.
//
// The values are meant to be overridden at build time with
// -ldflags "-X sklad/server/internal/buildinfo.Version=1.0.0", together with
// the Commit and BuildTime keys; see the Dockerfile.
package buildinfo

// Version is the semantic version of the build ("dev" outside a release).
var Version = "dev"

// Commit is the short Git revision of the build ("none" outside a release).
var Commit = "none"

// BuildTime is the UTC build timestamp in RFC 3339 ("unknown" outside a release).
var BuildTime = "unknown"

// String returns a compact human-readable summary, for example "1.0.0 (a1b2c3d)".
func String() string {
	if Commit == "" || Commit == "none" {
		return Version
	}
	return Version + " (" + Commit + ")"
}
