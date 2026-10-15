package upgrade

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gotcli/got/internal/toolchain"
)

var (
	goDirective        = regexp.MustCompile(`(?m)^go[ \t]+(\S+)[ \t]*$`)
	toolchainDirective = regexp.MustCompile(`(?m)^toolchain[ \t]+(\S+)[ \t]*$`)
	dockerGoImage      = regexp.MustCompile(`golang:([0-9]+)\.([0-9]+)(?:\.[0-9]+)?-alpine`)
)

// planToolchain moves builds off Go releases that no longer receive security
// fixes: it raises the toolchain directive of go.mod and the golang image of the
// Dockerfile. The go directive (language version) is left unchanged.
func planToolchain(project, _ string) (Status, string, map[string][]byte, error) {
	changes := map[string][]byte{}
	goMod, err := os.ReadFile(filepath.Join(project, "go.mod"))
	if err != nil {
		return "", "", nil, fmt.Errorf("read go.mod: %w", err)
	}
	if updated, changed := raiseToolchain(goMod); changed {
		changes["go.mod"] = updated
	}
	dockerfile, err := os.ReadFile(filepath.Join(project, "Dockerfile"))
	if err != nil && !os.IsNotExist(err) {
		return "", "", nil, err
	}
	if err == nil {
		if updated, changed := raiseDockerImage(dockerfile); changed {
			changes["Dockerfile"] = updated
		}
	}
	if len(changes) == 0 {
		return StatusUpToDate, "", nil, nil
	}
	return StatusPending, "", changes, nil
}

// PlanWorkspace evaluates workspace-level files. In workspace mode the go.work
// toolchain directive, not each go.mod, selects the toolchain.
func PlanWorkspace(root string) ([]Result, error) {
	content, err := os.ReadFile(filepath.Join(root, "go.work"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := Result{ID: "go-toolchain-security", Description: toolchainDescription, Status: StatusUpToDate}
	if updated, changed := raiseToolchain(content); changed {
		result.Status = StatusPending
		result.Changes = map[string][]byte{"go.work": updated}
		result.Files = []string{"go.work"}
	}
	return []Result{result}, nil
}

const toolchainDescription = "build with a supported Go release (toolchain " + toolchain.GoToolchain + ", golang:" + toolchain.DockerImage + " image)"

func raiseToolchain(content []byte) ([]byte, bool) {
	minimum := parseGoVersion(toolchain.GoToolchain)
	if match := goDirective.FindSubmatch(content); match != nil && !lessVersion(parseGoVersion(string(match[1])), minimum) {
		return content, false // the language version already requires a newer toolchain
	}
	line := "toolchain " + toolchain.GoToolchain
	if match := toolchainDirective.FindSubmatchIndex(content); match != nil {
		if !lessVersion(parseGoVersion(string(content[match[2]:match[3]])), minimum) {
			return content, false
		}
		return append(append(append([]byte{}, content[:match[0]]...), line...), content[match[1]:]...), true
	}
	match := goDirective.FindIndex(content)
	if match == nil {
		return content, false
	}
	return append(append(append([]byte{}, content[:match[1]]...), "\n\n"+line...), content[match[1]:]...), true
}

func raiseDockerImage(content []byte) ([]byte, bool) {
	minimumMinor := toolchain.MinimumMinor
	changed := false
	updated := dockerGoImage.ReplaceAllFunc(content, func(image []byte) []byte {
		parts := dockerGoImage.FindSubmatch(image)
		major, _ := strconv.Atoi(string(parts[1]))
		minor, _ := strconv.Atoi(string(parts[2]))
		if major > 1 || minor >= minimumMinor {
			return image
		}
		changed = true
		return []byte("golang:" + toolchain.DockerImage + "-alpine")
	})
	return updated, changed
}

// parseGoVersion reads "go1.26.9", "1.26.0", or "1.26" as [major, minor, patch].
func parseGoVersion(value string) [3]int {
	var version [3]int
	for i, part := range strings.SplitN(strings.TrimPrefix(value, "go"), ".", 3) {
		digits := strings.TrimRightFunc(part, func(r rune) bool { return r < '0' || r > '9' })
		version[i], _ = strconv.Atoi(digits)
	}
	return version
}

func lessVersion(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
