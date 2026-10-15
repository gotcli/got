package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNoInputListsEveryMissingFlag(t *testing.T) {
	tests := []struct {
		args     []string
		expected string
	}{
		{args: []string{"init", "--no-input", "--name", "catalog"}, expected: "missing required flags: --architecture, --module, --db"},
		{args: []string{"init", "--no-input", "--name", "catalog", "--module", "example.com/catalog", "--architecture", "standard", "--db", "pg", "--generate-crud", "--db-name", "catalog"}, expected: "missing required flags: --db-host, --db-user"},
		{args: []string{"init", "workspace", "--no-input", "--db", "pg"}, expected: "missing required flags: --name, --module, --services"},
	}
	for _, test := range tests {
		command := NewRootCommand("test")
		command.SetArgs(test.args)
		err := command.Execute()
		if err == nil || !strings.Contains(err.Error(), test.expected) {
			t.Errorf("got %v for %v, want error containing %q", err, test.args, test.expected)
		}
	}
}

func TestReadDatabasePassword(t *testing.T) {
	t.Run("environment wins without prompting", func(t *testing.T) {
		t.Setenv(databasePasswordEnv, "from-env")
		prompt := &passwordPromptStub{password: "typed"}
		password, err := readDatabasePassword(&config{nonInteractive: true}, prompt)
		if err != nil || password != "from-env" || prompt.calls != 0 {
			t.Fatalf("password=%q err=%v calls=%d", password, err, prompt.calls)
		}
	})
	t.Run("non-interactive without environment fails", func(t *testing.T) {
		t.Setenv(databasePasswordEnv, "")
		prompt := &passwordPromptStub{password: "typed"}
		_, err := readDatabasePassword(&config{nonInteractive: true}, prompt)
		if err == nil || !strings.Contains(err.Error(), databasePasswordEnv) || prompt.calls != 0 {
			t.Fatalf("err=%v calls=%d", err, prompt.calls)
		}
	})
	t.Run("interactive prompts", func(t *testing.T) {
		t.Setenv(databasePasswordEnv, "")
		prompt := &passwordPromptStub{password: "typed"}
		password, err := readDatabasePassword(&config{}, prompt)
		if err != nil || password != "typed" || prompt.calls != 1 {
			t.Fatalf("password=%q err=%v calls=%d", password, err, prompt.calls)
		}
	})
}

func TestLayeredGeneratorsRejectHexagonalProjects(t *testing.T) {
	project := t.TempDir()
	writeDoctorFile(t, filepath.Join(project, "got-project.json"), `{"architecture":"hexagonal"}`)
	for _, args := range [][]string{{"api", "--name", "users"}, {"add", "upload"}, {"add", "method", "--folder", "orders", "--name", "Approve"}} {
		command := NewRootCommand("test")
		command.SetArgs(args)
		t.Chdir(project)
		err := command.Execute()
		if err == nil || !strings.Contains(err.Error(), "does not support hexagonal projects") {
			t.Errorf("%v: got %v", args, err)
		}
	}
}
