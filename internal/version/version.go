// Package version is populated at link time via -ldflags by the Makefile and
// release workflow. The values are surfaced to the Pulumi engine and embedded
// in the JSON schema document.
package version

// Build-time variables. Default to "dev" when built without ldflags.
var (
	// Version is the SemVer 2.0.0 string of the build, e.g. v1.0.0 or
	// v1.2.3-rc.1+sha.<short>.
	Version = "dev"

	// GitCommit is the short Git revision (7 hex digits).
	GitCommit = "unknown"

	// BuildDate is the RFC 3339 UTC timestamp at which the binary was built.
	BuildDate = "unknown"
)

// Full returns a single-line version string suitable for "-version" output.
func Full() string {
	return "version=" + Version + " commit=" + GitCommit + " built=" + BuildDate
}
