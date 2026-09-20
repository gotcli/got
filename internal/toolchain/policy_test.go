package toolchain

import "testing"

func TestGoVersionPolicyIsConsistent(t *testing.T) {
	if GoVersion != "1.25.0" {
		t.Fatalf("GoVersion = %q", GoVersion)
	}
	if GoToolchain != "go"+GoVersion {
		t.Fatalf("GoToolchain = %q, want go%s", GoToolchain, GoVersion)
	}
	if MinimumMinor != 25 {
		t.Fatalf("MinimumMinor = %d", MinimumMinor)
	}
}
