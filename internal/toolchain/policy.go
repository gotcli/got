// Package toolchain defines the Go version policy used by GOT and the projects
// it generates.
//
// Generated projects target the oldest Go release that still receives security
// fixes. Go supports the two most recent major releases only, so this policy
// moves when a release reaches end of life.
package toolchain

const (
	// GoVersion is the go directive (language version) of generated modules.
	GoVersion = "1.26.0"
	// GoToolchain is the minimum toolchain for generated modules: the latest
	// patch of GoVersion's release, so builds include its security fixes.
	GoToolchain = "go1.26.9"
	// DockerImage is the golang image tag for generated Dockerfiles. It names
	// the release, not a patch, so rebuilding an image picks up new patches.
	DockerImage = "1.26"
	// MinimumMinor is the oldest Go 1.x release accepted by "got doctor".
	MinimumMinor = 26
)
