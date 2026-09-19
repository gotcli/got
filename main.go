package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/gotcli/got-community/cmd"
)

// version may be overridden by release builds with -ldflags "-X main.version=vX.Y.Z".
// Tagged module installs obtain their version from Go build information.
var version string

func main() {
	if err := cmd.Execute(resolveVersion()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func resolveVersion() string {
	if version != "" {
		return version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	return versionFromBuildInfo(info)
}

func versionFromBuildInfo(info *debug.BuildInfo) string {
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
