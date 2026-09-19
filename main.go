package main

import (
	"fmt"
	"os"

	"github.com/gotcli/got-community/cmd"
)

var version = "dev"

func main() {
	if err := cmd.Execute(version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
