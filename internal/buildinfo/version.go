package buildinfo

import (
	"runtime/debug"
	"strings"
)

// Version returns an explicit release version when supplied, otherwise it
// derives the tagged module version or source revision from Go build metadata.
func Version(override string) string {
	if override != "" {
		return override
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	return FromBuildInfo(info)
}

// FromBuildInfo converts Go build metadata into a user-facing CLI version.
func FromBuildInfo(info *debug.BuildInfo) string {
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}

	var revision string
	modified := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return "dev"
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}

	result := "dev+" + strings.ToLower(revision)
	if modified {
		result += ".dirty"
	}
	return result
}
