package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

type configEntry struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source"`
	Secret bool   `json:"secret,omitempty"`
}

type serviceConfig struct {
	Service   string        `json:"service,omitempty"`
	EnvPrefix string        `json:"envPrefix"`
	Entries   []configEntry `json:"entries"`
}

func newConfigCommand(cfg *config) *cobra.Command {
	var asJSON bool
	command := &cobra.Command{
		Use:   "config",
		Short: "Show the effective configuration of the current project",
		Long: `Show the configuration a generated service will use, after environment
variables override config.yml, and where each value comes from.

Generated services read config.yml and let an environment variable named
<PREFIX>_<KEY> override any key. The prefix is the last element of the module
path, upper-cased, with "-" and "." replaced by "_". Empty environment
variables are ignored, as Viper ignores them. Values of keys that look secret
(PASSWORD, SECRET, TOKEN, KEY) are never printed. From a workspace root, every
service is shown.`,
		Example: `  got config
  SHOP_DB_HOST=db.internal got config
  got config --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			projects, workspace, err := discoverDoctorProjects(cfg.cwd)
			if err != nil {
				return err
			}
			results := make([]serviceConfig, 0, len(projects))
			for _, project := range projects {
				result, err := effectiveConfig(project.Path, project.Module)
				if err != nil {
					return err
				}
				if workspace {
					result.Service = filepath.Base(project.Path)
				}
				results = append(results, result)
			}
			if asJSON {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				if workspace {
					return encoder.Encode(results)
				}
				return encoder.Encode(results[0])
			}
			for index, result := range results {
				if index > 0 {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				if result.Service != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "# %s\n", result.Service)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "environment prefix: %s_\n\n", result.EnvPrefix)
				writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
				fmt.Fprintln(writer, "KEY\tVALUE\tSOURCE")
				for _, entry := range result.Entries {
					fmt.Fprintf(writer, "%s\t%s\t%s\n", entry.Key, entry.Value, entry.Source)
				}
				if err := writer.Flush(); err != nil {
					return err
				}
			}
			return nil
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "print configuration as JSON")
	return command
}

func effectiveConfig(projectPath, module string) (serviceConfig, error) {
	keys, values, err := readOrderedYAML(filepath.Join(projectPath, "config.yml"))
	if err != nil {
		return serviceConfig{}, err
	}
	prefix := generatedEnvPrefix(module, filepath.Base(projectPath))
	result := serviceConfig{EnvPrefix: prefix}
	inFile := make(map[string]bool, len(keys))
	for _, key := range keys {
		inFile[strings.ToUpper(key)] = true
		entry := configEntry{Key: key, Value: values[key], Source: "config.yml"}
		envName := prefix + "_" + strings.ToUpper(key)
		if value := os.Getenv(envName); value != "" {
			entry.Value = value
			entry.Source = "env " + envName + " (overrides config.yml)"
		}
		result.Entries = append(result.Entries, maskSecret(entry))
	}
	// Keys set only through the environment are still read by viper.Get*.
	var envOnly []configEntry
	for _, variable := range os.Environ() {
		name, value, _ := strings.Cut(variable, "=")
		key, ok := strings.CutPrefix(name, prefix+"_")
		if !ok || key == "" || value == "" || inFile[key] {
			continue
		}
		envOnly = append(envOnly, maskSecret(configEntry{Key: key, Value: value, Source: "env " + name}))
	}
	sort.Slice(envOnly, func(i, j int) bool { return envOnly[i].Key < envOnly[j].Key })
	result.Entries = append(result.Entries, envOnly...)
	return result, nil
}

func isSecretKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, marker := range []string{"PASSWORD", "SECRET", "TOKEN", "PRIVATE"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return upper == "KEY" || strings.HasSuffix(upper, "_KEY") || strings.HasPrefix(upper, "KEY_")
}

func maskSecret(entry configEntry) configEntry {
	if !isSecretKey(entry.Key) {
		return entry
	}
	entry.Secret = true
	if entry.Value == "" {
		entry.Value = "(not set)"
	} else {
		entry.Value = "******** (set)"
	}
	return entry
}
