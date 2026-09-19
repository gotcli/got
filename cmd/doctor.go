package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const minimumGoMinor = 22

type doctorStatus string

const (
	doctorPass doctorStatus = "PASS"
	doctorWarn doctorStatus = "WARN"
	doctorFail doctorStatus = "FAIL"
)

type doctorCheck struct {
	Status  doctorStatus `json:"status"`
	Name    string       `json:"name"`
	Message string       `json:"message,omitempty"`
}

type doctorReport struct {
	Checks  []doctorCheck `json:"checks"`
	Summary doctorSummary `json:"summary"`
}

type doctorSummary struct {
	Passed   int `json:"passed"`
	Warnings int `json:"warnings"`
	Failed   int `json:"failed"`
}

type doctorOptions struct {
	Project bool
	Connect bool
	JSON    bool
}

type doctorDependencies struct {
	lookPath func(string) (string, error)
	command  func(context.Context, string, ...string) ([]byte, error)
	dial     func(context.Context, string, string) (net.Conn, error)
	httpGet  func(context.Context, string) (*http.Response, error)
}

func defaultDoctorDependencies() doctorDependencies {
	return doctorDependencies{
		lookPath: exec.LookPath,
		command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		},
		dial: (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
		httpGet: func(ctx context.Context, url string) (*http.Response, error) {
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return nil, err
			}
			return (&http.Client{Timeout: 3 * time.Second}).Do(request)
		},
	}
}

func newDoctorCommand(cfg *config, version string) *cobra.Command {
	return newDoctorCommandWithDependencies(cfg, version, defaultDoctorDependencies())
}

func newDoctorCommandWithDependencies(cfg *config, version string, dependencies doctorDependencies) *cobra.Command {
	var options doctorOptions
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Check whether GOT and the current project are ready",
		Long: `Check the local development environment and print actionable results.

The default checks are local and do not contact external systems. Use --project
inside a generated service or workspace to validate project files, configuration,
secrets, and writable runtime directories. Use --connect to additionally check
Go module DNS, the configured database TCP endpoint, and the local readiness URL.
Secret values are never printed. Use --json in CI or automation.`,
		Example: `  got doctor
  got doctor --project
  got doctor --project --connect
  got doctor --project --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report := runDoctor(cmd.Context(), cfg.cwd, version, options, dependencies)
			if options.JSON {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(report); err != nil {
					return err
				}
			} else {
				writeDoctorReport(cmd.OutOrStdout(), report)
			}
			if report.Summary.Failed > 0 {
				return fmt.Errorf("doctor found %d failed check(s)", report.Summary.Failed)
			}
			return nil
		},
	}
	command.Flags().BoolVar(&options.Project, "project", false, "check the generated service or workspace in the current directory")
	command.Flags().BoolVar(&options.Connect, "connect", false, "perform read-only network, database endpoint, and health checks")
	command.Flags().BoolVar(&options.JSON, "json", false, "print a machine-readable JSON report")
	return command
}

func runDoctor(ctx context.Context, cwd, version string, options doctorOptions, dependencies doctorDependencies) doctorReport {
	checks := []doctorCheck{{Status: doctorPass, Name: "GOT version", Message: version}}
	checks = append(checks, checkToolchain(ctx, dependencies)...)
	checks = append(checks, checkWritableDirectory(cwd))
	checks = append(checks, checkPort(3000))
	if options.Connect {
		checks = append(checks, checkModuleDNS(ctx))
	}
	if options.Project {
		checks = append(checks, checkProject(ctx, cwd, options.Connect, dependencies)...)
	}
	return summarizeDoctor(checks)
}

func checkToolchain(ctx context.Context, dependencies doctorDependencies) []doctorCheck {
	checks := make([]doctorCheck, 0, 4)
	if _, err := dependencies.lookPath("go"); err != nil {
		checks = append(checks, doctorCheck{Status: doctorFail, Name: "Go", Message: "not found in PATH; install Go 1.22 or newer"})
	} else {
		output, err := dependencies.command(ctx, "go", "version")
		version := strings.TrimSpace(string(output))
		if err != nil {
			checks = append(checks, doctorCheck{Status: doctorFail, Name: "Go", Message: "could not read version"})
		} else if !supportedGoVersion(version) {
			checks = append(checks, doctorCheck{Status: doctorFail, Name: "Go", Message: version + "; Go 1.22 or newer is required"})
		} else {
			checks = append(checks, doctorCheck{Status: doctorPass, Name: "Go", Message: version})
		}
	}
	checks = append(checks, optionalTool(dependencies, "git", "Git"))
	checks = append(checks, optionalTool(dependencies, "docker", "Docker"))
	if _, err := dependencies.lookPath("docker"); err == nil {
		checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		output, commandErr := dependencies.command(checkCtx, "docker", "info", "--format", "{{.ServerVersion}}")
		checks = append(checks, dockerDaemonCheck(output, commandErr))
		cancel()

		checkCtx, cancel = context.WithTimeout(ctx, 3*time.Second)
		if output, err := dependencies.command(checkCtx, "docker", "compose", "version"); err != nil {
			checks = append(checks, doctorCheck{Status: doctorWarn, Name: "Docker Compose", Message: compactCommandError(output, err)})
		} else {
			checks = append(checks, doctorCheck{Status: doctorPass, Name: "Docker Compose", Message: strings.TrimSpace(string(output))})
		}
		cancel()
	}
	return checks
}

func dockerDaemonCheck(output []byte, err error) doctorCheck {
	message := strings.TrimSpace(string(output))
	lower := strings.ToLower(message)
	failedMessage := message == "" || strings.Contains(lower, "permission denied") || strings.Contains(lower, "cannot connect") || strings.Contains(lower, "error during connect")
	if err != nil || failedMessage {
		if err == nil {
			err = errors.New("Docker daemon is not reachable")
		}
		return doctorCheck{Status: doctorWarn, Name: "Docker daemon", Message: compactCommandError(output, err)}
	}
	return doctorCheck{Status: doctorPass, Name: "Docker daemon", Message: "running (server " + message + ")"}
}

func optionalTool(dependencies doctorDependencies, executable, label string) doctorCheck {
	path, err := dependencies.lookPath(executable)
	if err != nil {
		return doctorCheck{Status: doctorWarn, Name: label, Message: "not found in PATH"}
	}
	return doctorCheck{Status: doctorPass, Name: label, Message: path}
}

func supportedGoVersion(output string) bool {
	match := regexp.MustCompile(`go1\.(\d+)`).FindStringSubmatch(output)
	if len(match) != 2 {
		return false
	}
	minor, err := strconv.Atoi(match[1])
	return err == nil && minor >= minimumGoMinor
}

func checkWritableDirectory(path string) doctorCheck {
	file, err := os.CreateTemp(path, ".got-doctor-*")
	if err != nil {
		return doctorCheck{Status: doctorFail, Name: "Current directory", Message: "not writable: " + err.Error()}
	}
	name := file.Name()
	closeErr := file.Close()
	removeErr := os.Remove(name)
	if closeErr != nil || removeErr != nil {
		return doctorCheck{Status: doctorFail, Name: "Current directory", Message: "temporary file cleanup failed"}
	}
	return doctorCheck{Status: doctorPass, Name: "Current directory", Message: path + " is writable"}
}

func checkPort(port int) doctorCheck {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return doctorCheck{Status: doctorWarn, Name: fmt.Sprintf("Port %d", port), Message: "already in use"}
	}
	_ = listener.Close()
	return doctorCheck{Status: doctorPass, Name: fmt.Sprintf("Port %d", port), Message: "available"}
}

func checkModuleDNS(ctx context.Context) doctorCheck {
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, err := net.DefaultResolver.LookupHost(lookupCtx, "proxy.golang.org"); err != nil {
		return doctorCheck{Status: doctorWarn, Name: "Go module DNS", Message: err.Error()}
	}
	return doctorCheck{Status: doctorPass, Name: "Go module DNS", Message: "proxy.golang.org resolves"}
}

type doctorProject struct {
	Path   string
	Module string
}

func checkProject(ctx context.Context, cwd string, connect bool, dependencies doctorDependencies) []doctorCheck {
	projects, workspace, err := discoverDoctorProjects(cwd)
	if err != nil {
		return []doctorCheck{{Status: doctorFail, Name: "Generated project", Message: err.Error()}}
	}
	checks := []doctorCheck{{Status: doctorPass, Name: "Project type", Message: map[bool]string{true: "microservice workspace", false: "single service"}[workspace]}}
	for _, project := range projects {
		checks = append(checks, checkServiceProject(ctx, project, connect, dependencies)...)
	}
	return checks
}

func discoverDoctorProjects(cwd string) ([]doctorProject, bool, error) {
	manifestPath := filepath.Join(cwd, "got-workspace.json")
	if content, err := os.ReadFile(manifestPath); err == nil {
		var manifest struct {
			Services []string `json:"services"`
		}
		if err := json.Unmarshal(content, &manifest); err != nil {
			return nil, true, fmt.Errorf("invalid got-workspace.json: %w", err)
		}
		if len(manifest.Services) == 0 {
			return nil, true, errors.New("got-workspace.json has no services")
		}
		projects := make([]doctorProject, 0, len(manifest.Services))
		for _, service := range manifest.Services {
			path := filepath.Join(cwd, service)
			module, err := readModuleName(filepath.Join(path, "go.mod"))
			if err != nil {
				return nil, true, fmt.Errorf("%s: %w", service, err)
			}
			projects = append(projects, doctorProject{Path: path, Module: module})
		}
		return projects, true, nil
	} else if !os.IsNotExist(err) {
		return nil, false, err
	}
	module, err := readModuleName(filepath.Join(cwd, "go.mod"))
	if err != nil {
		return nil, false, errors.New("run --project from a generated service or workspace root containing go.mod or got-workspace.json")
	}
	return []doctorProject{{Path: cwd, Module: module}}, false, nil
}

func readModuleName(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	return "", errors.New("go.mod has no module directive")
}

func checkServiceProject(ctx context.Context, project doctorProject, connect bool, dependencies doctorDependencies) []doctorCheck {
	label := filepath.Base(project.Path)
	checks := []doctorCheck{{Status: doctorPass, Name: label + ": go.mod", Message: project.Module}}
	configPath := filepath.Join(project.Path, "config.yml")
	values, err := readSimpleYAML(configPath)
	if err != nil {
		return append(checks, doctorCheck{Status: doctorFail, Name: label + ": config.yml", Message: err.Error()})
	}
	checks = append(checks, doctorCheck{Status: doctorPass, Name: label + ": config.yml", Message: "found"})
	prefix := generatedEnvPrefix(project.Module, label)
	password := configValue(values, prefix, "DB_PASSWORD")
	if strings.TrimSpace(password) == "" {
		checks = append(checks, doctorCheck{Status: doctorFail, Name: label + ": database password", Message: prefix + "_DB_PASSWORD is not set"})
	} else {
		checks = append(checks, doctorCheck{Status: doctorPass, Name: label + ": database password", Message: "configured"})
	}
	checks = append(checks, checkRuntimeDirectory(label+": log directory", project.Path, configValue(values, prefix, "LOG_DIR"), "logs"))
	if _, hasUpload := values["UPLOAD_DIR"]; hasUpload {
		checks = append(checks, checkRuntimeDirectory(label+": upload directory", project.Path, configValue(values, prefix, "UPLOAD_DIR"), "uploads"))
	}
	if _, hasJWT := values["JWT_ACCESS_SECRET"]; hasJWT {
		checks = append(checks, checkJWTSecret(label+": JWT access secret", configValue(values, prefix, "JWT_ACCESS_SECRET")))
		checks = append(checks, checkJWTSecret(label+": JWT refresh secret", configValue(values, prefix, "JWT_REFRESH_SECRET")))
	}
	if connect {
		checks = append(checks, checkDatabaseEndpoint(ctx, label, values, prefix, dependencies))
		checks = append(checks, checkReadiness(ctx, label, values, prefix, dependencies))
	}
	return checks
}

func readSimpleYAML(path string) (map[string]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config.yml: %w", err)
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return values, nil
}

func configValue(values map[string]string, prefix, key string) string {
	if value, ok := os.LookupEnv(prefix + "_" + key); ok {
		return value
	}
	return values[key]
}

func checkRuntimeDirectory(name, root, configured, fallback string) doctorCheck {
	if strings.TrimSpace(configured) == "" {
		configured = fallback
	}
	if !filepath.IsAbs(configured) {
		configured = filepath.Join(root, configured)
	}
	probe := configured
	for {
		info, err := os.Stat(probe)
		if err == nil {
			if !info.IsDir() {
				return doctorCheck{Status: doctorFail, Name: name, Message: probe + " is not a directory"}
			}
			file, err := os.CreateTemp(probe, ".got-doctor-*")
			if err != nil {
				return doctorCheck{Status: doctorFail, Name: name, Message: "not writable: " + err.Error()}
			}
			fileName := file.Name()
			_ = file.Close()
			_ = os.Remove(fileName)
			return doctorCheck{Status: doctorPass, Name: name, Message: configured + " is writable"}
		}
		if !os.IsNotExist(err) {
			return doctorCheck{Status: doctorFail, Name: name, Message: err.Error()}
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return doctorCheck{Status: doctorFail, Name: name, Message: "no writable parent directory"}
		}
		probe = parent
	}
}

func checkJWTSecret(name, value string) doctorCheck {
	if len(value) < 32 {
		return doctorCheck{Status: doctorFail, Name: name, Message: "must be configured with at least 32 bytes"}
	}
	return doctorCheck{Status: doctorPass, Name: name, Message: "configured"}
}

func checkDatabaseEndpoint(ctx context.Context, label string, values map[string]string, prefix string, dependencies doctorDependencies) doctorCheck {
	host := configValue(values, prefix, "DB_HOST")
	port := configValue(values, prefix, "DB_PORT")
	if host == "" || port == "" {
		return doctorCheck{Status: doctorFail, Name: label + ": database endpoint", Message: "DB_HOST or DB_PORT is missing"}
	}
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	connection, err := dependencies.dial(checkCtx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return doctorCheck{Status: doctorFail, Name: label + ": database endpoint", Message: err.Error()}
	}
	_ = connection.Close()
	return doctorCheck{Status: doctorPass, Name: label + ": database endpoint", Message: net.JoinHostPort(host, port) + " is reachable"}
}

func checkReadiness(ctx context.Context, label string, values map[string]string, prefix string, dependencies doctorDependencies) doctorCheck {
	port := configValue(values, prefix, "APP_PORT")
	if port == "" {
		port = "3000"
	}
	url := "http://127.0.0.1:" + port + "/health/ready"
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	response, err := dependencies.httpGet(checkCtx, url)
	if err != nil {
		return doctorCheck{Status: doctorWarn, Name: label + ": readiness endpoint", Message: "not reachable at " + url}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return doctorCheck{Status: doctorFail, Name: label + ": readiness endpoint", Message: response.Status}
	}
	return doctorCheck{Status: doctorPass, Name: label + ": readiness endpoint", Message: response.Status}
}

func summarizeDoctor(checks []doctorCheck) doctorReport {
	report := doctorReport{Checks: checks}
	for _, check := range checks {
		switch check.Status {
		case doctorPass:
			report.Summary.Passed++
		case doctorWarn:
			report.Summary.Warnings++
		case doctorFail:
			report.Summary.Failed++
		}
	}
	return report
}

func writeDoctorReport(writer io.Writer, report doctorReport) {
	fmt.Fprintln(writer, "GOT Doctor")
	fmt.Fprintln(writer)
	for _, check := range report.Checks {
		if check.Message == "" {
			fmt.Fprintf(writer, "[%s] %s\n", check.Status, check.Name)
		} else {
			fmt.Fprintf(writer, "[%s] %s: %s\n", check.Status, check.Name, check.Message)
		}
	}
	fmt.Fprintln(writer)
	fmt.Fprintf(writer, "Summary: %d passed, %d warning(s), %d failed\n", report.Summary.Passed, report.Summary.Warnings, report.Summary.Failed)
}

func compactCommandError(output []byte, err error) string {
	lines := strings.Fields(strings.TrimSpace(string(output)))
	if len(lines) > 0 {
		if len(lines) > 12 {
			lines = lines[:12]
		}
		return strings.Join(lines, " ")
	}
	return err.Error()
}
