package gitworkspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"default_branch":"main"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.DefaultBranch != "main" || config.Version != CurrentConfigVersion {
		t.Fatalf("unexpected config: %#v", config)
	}
}

func TestConfigRejectsUnsupportedVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"default_branch":"main"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); ErrorCode(err) != "config_invalid" {
		t.Fatalf("error=%v", err)
	}
}

func TestConfigRejectsInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); ErrorCode(err) != "config_invalid" {
		t.Fatalf("error=%v", err)
	}
}

func TestMissingConfigIsOptional(t *testing.T) {
	config, err := LoadConfig(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if config.Version != CurrentConfigVersion || config.DefaultBranch != "" {
		t.Fatalf("config=%#v", config)
	}
}

func TestSaveConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := SaveConfig(path, ProjectConfig{DefaultBranch: "develop"}); err != nil {
		t.Fatal(err)
	}
	config, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.DefaultBranch != "develop" {
		t.Fatalf("config=%#v", config)
	}
}
