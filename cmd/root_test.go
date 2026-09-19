package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	command := NewRootCommand("v1.2.3")
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"version"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != "v1.2.3" {
		t.Fatalf("unexpected output %q", output.String())
	}
}

func TestCommandHelpIncludesWorkflowsAndExamples(t *testing.T) {
	tests := []struct {
		args     []string
		expected []string
	}{
		{args: []string{"--help"}, expected: []string{"standard API or microservice", "got init --name ordering", "got add method"}},
		{args: []string{"init", "--help"}, expected: []string{"--generate-crud", "masked prompt", "health endpoints", "sql.internal"}},
		{args: []string{"init", "service", "--help"}, expected: []string{"independently deployable service", "--architecture"}},
		{args: []string{"init", "workspace", "--help"}, expected: []string{"go.work", "--services", "restaurant-platform"}},
		{args: []string{"api", "--help"}, expected: []string{"intentional TODO scaffolds", "got api --name order_items"}},
		{args: []string{"add", "method", "--help"}, expected: []string{"repository, service, handler", "--http-method PATCH", "TODO panic"}},
		{args: []string{"add", "auth", "--help"}, expected: []string{"access/refresh token", "environment variables", "got add auth --jwt"}},
		{args: []string{"add", "upload", "--help"}, expected: []string{"POST /api/uploads", "random names", "10 MiB"}},
		{args: []string{"add", "swagger", "--help"}, expected: []string{"OpenAPI 3.0", "/swagger/index.html", "APP_ENV is production"}},
		{args: []string{"add", "service", "--help"}, expected: []string{"got-workspace.json", "got add service payment"}},
		{args: []string{"doctor", "--help"}, expected: []string{"--project", "--connect", "--json", "Secret values are never printed"}},
		{args: []string{"generate", "crud", "--help"}, expected: []string{"current project's database", "masked prompt", "workspace root", "commerce-platform/order-service", "--db-schema"}},
	}
	for _, test := range tests {
		command := NewRootCommand("test")
		var output bytes.Buffer
		command.SetOut(&output)
		command.SetErr(&output)
		command.SetArgs(test.args)
		if err := command.Execute(); err != nil {
			t.Fatalf("got %v: %v", test.args, err)
		}
		for _, expected := range test.expected {
			if !strings.Contains(output.String(), expected) {
				t.Errorf("got %v help does not contain %q:\n%s", test.args, expected, output.String())
			}
		}
	}
}
