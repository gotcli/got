package cmd

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSupportedGoVersion(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: "go version go1.22.1 darwin/arm64", want: true},
		{value: "go version go1.25.0 linux/amd64", want: true},
		{value: "go version go1.21.9 windows/amd64", want: false},
		{value: "unexpected", want: false},
	}
	for _, test := range tests {
		if got := supportedGoVersion(test.value); got != test.want {
			t.Errorf("supportedGoVersion(%q)=%v, want %v", test.value, got, test.want)
		}
	}
}

func TestSummarizeDoctor(t *testing.T) {
	report := summarizeDoctor([]doctorCheck{
		{Status: doctorPass, Name: "one"},
		{Status: doctorPass, Name: "two"},
		{Status: doctorWarn, Name: "three"},
		{Status: doctorFail, Name: "four"},
	})
	if report.Summary.Passed != 2 || report.Summary.Warnings != 1 || report.Summary.Failed != 1 {
		t.Fatalf("unexpected summary: %+v", report.Summary)
	}
}

func TestDockerDaemonCheckDetectsErrorTextWithZeroExitCode(t *testing.T) {
	check := dockerDaemonCheck([]byte("permission denied while trying to connect to the Docker daemon socket"), nil)
	if check.Status != doctorWarn {
		t.Fatalf("status=%s, want WARN", check.Status)
	}
	check = dockerDaemonCheck([]byte("27.1.1"), nil)
	if check.Status != doctorPass {
		t.Fatalf("status=%s, want PASS", check.Status)
	}
}

func TestDoctorProjectDoesNotExposeSecrets(t *testing.T) {
	root := t.TempDir()
	writeDoctorFile(t, filepath.Join(root, "go.mod"), "module example.com/customer-api\n\ngo 1.22\n")
	writeDoctorFile(t, filepath.Join(root, "config.yml"), `APP_PORT: 3000
LOG_DIR: "logs"
DB_HOST: "localhost"
DB_PORT: 5432
DB_PASSWORD: ""
JWT_ACCESS_SECRET: ""
JWT_REFRESH_SECRET: ""
`)
	accessSecret := "access-secret-that-is-longer-than-32-bytes"
	refreshSecret := "refresh-secret-that-is-longer-than-32-bytes"
	t.Setenv("CUSTOMER_API_DB_PASSWORD", "database-secret")
	t.Setenv("CUSTOMER_API_JWT_ACCESS_SECRET", accessSecret)
	t.Setenv("CUSTOMER_API_JWT_REFRESH_SECRET", refreshSecret)

	checks := checkProject(context.Background(), root, false, doctorDependencies{})
	report := summarizeDoctor(checks)
	if report.Summary.Failed != 0 {
		t.Fatalf("unexpected failed checks: %+v", checks)
	}
	var output bytes.Buffer
	writeDoctorReport(&output, report)
	for _, secret := range []string{"database-secret", accessSecret, refreshSecret} {
		if strings.Contains(output.String(), secret) {
			t.Fatalf("doctor output exposed secret %q", secret)
		}
	}
}

func TestDiscoverDoctorWorkspace(t *testing.T) {
	root := t.TempDir()
	writeDoctorFile(t, filepath.Join(root, "got-workspace.json"), `{"services":["order-service","auth-service"]}`)
	writeDoctorFile(t, filepath.Join(root, "order-service", "go.mod"), "module example.com/platform/order-service\n")
	writeDoctorFile(t, filepath.Join(root, "auth-service", "go.mod"), "module example.com/platform/auth-service\n")

	projects, workspace, err := discoverDoctorProjects(root)
	if err != nil {
		t.Fatal(err)
	}
	if !workspace || len(projects) != 2 {
		t.Fatalf("workspace=%v projects=%+v", workspace, projects)
	}
}

func TestDoctorCommandJSONAndFailureExit(t *testing.T) {
	root := t.TempDir()
	writeDoctorFile(t, filepath.Join(root, "go.mod"), "module example.com/missing-password\n")
	writeDoctorFile(t, filepath.Join(root, "config.yml"), "DB_HOST: localhost\nDB_PORT: 5432\n")
	dependencies := doctorDependencies{
		lookPath: func(name string) (string, error) {
			if name == "docker" {
				return "", errors.New("missing")
			}
			return "/bin/" + name, nil
		},
		command: func(_ context.Context, name string, _ ...string) ([]byte, error) {
			if name == "go" {
				return []byte("go version go1.22.1 test/arch"), nil
			}
			return nil, nil
		},
		dial:    func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("unused") },
		httpGet: func(context.Context, string) (*http.Response, error) { return nil, errors.New("unused") },
	}
	command := newDoctorCommandWithDependencies(&config{cwd: root}, "v1.0.0", dependencies)
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--project", "--json"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "failed check") {
		t.Fatalf("expected failed doctor exit, got %v", err)
	}
	for _, expected := range []string{`"status": "FAIL"`, `"failed": 1`, "MISSING_PASSWORD_DB_PASSWORD"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("JSON output does not contain %q:\n%s", expected, output.String())
		}
	}
}

func writeDoctorFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
