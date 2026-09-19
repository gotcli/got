package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestFromBuildInfo(t *testing.T) {
	tests := []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{
			name: "tagged module",
			info: &debug.BuildInfo{Main: debug.Module{Version: "v1.2.3"}},
			want: "v1.2.3",
		},
		{
			name: "development commit",
			info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "ABCDEF0123456789"},
			}},
			want: "dev+abcdef012345",
		},
		{
			name: "dirty development commit",
			info: &debug.BuildInfo{Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abcdef0123456789"},
				{Key: "vcs.modified", Value: "true"},
			}},
			want: "dev+abcdef012345.dirty",
		},
		{
			name: "missing metadata",
			info: &debug.BuildInfo{},
			want: "dev",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := FromBuildInfo(test.info); got != test.want {
				t.Fatalf("FromBuildInfo() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestVersionPrefersOverride(t *testing.T) {
	if got := Version("v9.9.9"); got != "v9.9.9" {
		t.Fatalf("Version() = %q, want v9.9.9", got)
	}
}
