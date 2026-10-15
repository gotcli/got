package cmd

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigCommandShowsSourcesAndMasksSecrets(t *testing.T) {
	project := filepath.Join(t.TempDir(), "order-api")
	writeDoctorFile(t, filepath.Join(project, "go.mod"), "module example.com/order-api\n")
	writeDoctorFile(t, filepath.Join(project, "config.yml"), "APP_PORT: 3000\nDB_HOST: \"localhost\"\nDB_PASSWORD: \"file-secret\"\n")
	t.Setenv("ORDER_API_DB_HOST", "db.internal")
	t.Setenv("ORDER_API_APP_PORT", "") // empty values are ignored, as Viper does
	t.Setenv("ORDER_API_JWT_ACCESS_SECRET", "env-secret")
	t.Setenv("ORDER_API_FEATURE_FLAG", "on")

	command := newConfigCommand(&config{cwd: project})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--json"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "file-secret") || strings.Contains(output.String(), "env-secret") {
		t.Fatalf("secret value printed:\n%s", output.String())
	}
	var result serviceConfig
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	got := map[string]configEntry{}
	var order []string
	for _, entry := range result.Entries {
		got[entry.Key] = entry
		order = append(order, entry.Key)
	}
	if result.EnvPrefix != "ORDER_API" {
		t.Errorf("prefix = %q", result.EnvPrefix)
	}
	if strings.Join(order, ",") != "APP_PORT,DB_HOST,DB_PASSWORD,FEATURE_FLAG,JWT_ACCESS_SECRET" {
		t.Errorf("order = %v", order)
	}
	if entry := got["APP_PORT"]; entry.Value != "3000" || entry.Source != "config.yml" {
		t.Errorf("APP_PORT = %+v", entry)
	}
	if entry := got["DB_HOST"]; entry.Value != "db.internal" || entry.Source != "env ORDER_API_DB_HOST (overrides config.yml)" {
		t.Errorf("DB_HOST = %+v", entry)
	}
	if entry := got["DB_PASSWORD"]; !entry.Secret || entry.Value != "******** (set)" {
		t.Errorf("DB_PASSWORD = %+v", entry)
	}
	if entry := got["FEATURE_FLAG"]; entry.Value != "on" || entry.Source != "env ORDER_API_FEATURE_FLAG" {
		t.Errorf("FEATURE_FLAG = %+v", entry)
	}
}
