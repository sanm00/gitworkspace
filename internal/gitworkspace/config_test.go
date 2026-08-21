package gitworkspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProjectConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".git-workspace.yaml")
	content := "version: 1\nproject:\n  name: Demo\n  default_branch: main\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadProjectConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.Name != "Demo" || config.DefaultBranch != "main" {
		t.Fatalf("unexpected config: %#v", config)
	}
}

func TestConfigRejectsRemovedToolSections(t *testing.T) {
	for _, section := range []string{"agent", "editor"} {
		path := filepath.Join(t.TempDir(), ".git-workspace.yaml")
		if err := os.WriteFile(path, []byte("version: 1\n"+section+":\n  command: ignored\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadProjectConfig(path); ErrorCode(err) != "config_invalid" {
			t.Fatalf("section=%s error=%v", section, err)
		}
	}
}

func TestConfigRejectsRuntime(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".git-workspace.yaml")
	_ = os.WriteFile(path, []byte("version: 1\nruntime:\n  provider: none\n"), 0o600)
	_, err := LoadProjectConfig(path)
	if ErrorCode(err) != "config_invalid" {
		t.Fatalf("error=%v", err)
	}
}

func TestMissingConfigIsOptional(t *testing.T) {
	config, err := LoadProjectConfig(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if config.Version != 1 {
		t.Fatalf("version=%d", config.Version)
	}
}
