package main

import (
	"fmt"
	"os"

	"github.com/gotcli/got/cmd"
	"github.com/gotcli/got/internal/buildinfo"
)

// version may be overridden by release builds with -ldflags "-X main.version=vX.Y.Z".
// Tagged module installs obtain their version from Go build information.
var version string

func main() {
	if err := cmd.Execute(buildinfo.Version(version)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
