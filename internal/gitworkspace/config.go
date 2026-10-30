package gitworkspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

const CurrentConfigVersion = 1

type ProjectConfig struct {
	Version       int    `json:"version"`
	DefaultBranch string `json:"default_branch,omitempty"`
}

func LoadConfig(path string) (ProjectConfig, error) {
	config := ProjectConfig{Version: CurrentConfigVersion}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return config, WrapError("config_invalid", "Failed to read Git Workspace configuration.", err)
	}
	var raw struct {
		Version       *int    `json:"version"`
		DefaultBranch *string `json:"default_branch"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return config, configError(path, fmt.Sprintf("invalid JSON: %v", err))
	}
	if raw.Version != nil {
		config.Version = *raw.Version
	}
	if raw.DefaultBranch != nil {
		config.DefaultBranch = *raw.DefaultBranch
	}
	if config.Version != CurrentConfigVersion {
		return config, configError(path, fmt.Sprintf("unsupported version %d", config.Version))
	}
	return config, nil
}

func SaveConfig(path string, config ProjectConfig) error {
	if config.Version == 0 {
		config.Version = CurrentConfigVersion
	}
	return atomicJSON(path, config)
}

func configError(path, message string) error {
	err := NewError("config_invalid", fmt.Sprintf("Invalid Git Workspace configuration at %s: %s.", path, message))
	err.Details["path"] = path
	return err
}
