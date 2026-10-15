package toolchain

import (
	"strings"
	"testing"
)

func TestGoVersionPolicyIsConsistent(t *testing.T) {
	if GoVersion != "1.26.0" {
		t.Fatalf("GoVersion = %q", GoVersion)
	}
	release := strings.TrimSuffix(GoVersion, ".0")
	if !strings.HasPrefix(GoToolchain, "go"+release+".") {
		t.Fatalf("GoToolchain = %q, want a go%s.x patch", GoToolchain, release)
	}
	if DockerImage != release {
		t.Fatalf("DockerImage = %q, want %q", DockerImage, release)
	}
	if MinimumMinor != 26 {
		t.Fatalf("MinimumMinor = %d", MinimumMinor)
	}
}
