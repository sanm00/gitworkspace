package gitworkspace

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

func LoadProjectConfig(path string) (ProjectConfig, error) {
	config := ProjectConfig{Version: 1}
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return config, WrapError("config_invalid", "Failed to read project configuration.", err)
	}
	defer file.Close()

	section := ""
	scanner := bufio.NewScanner(file)
	lineNo := 0
	seenVersion := false
	for scanner.Scan() {
		lineNo++
		raw := scanner.Text()
		if strings.Contains(raw, "\t") {
			return config, configLine(path, lineNo, "tabs are not supported")
		}
		trimmed := strings.TrimSpace(stripComment(raw))
		if trimmed == "" {
			continue
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			return config, configLine(path, lineNo, "expected key: value")
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if indent == 0 {
			section = ""
			switch key {
			case "version":
				parsed, parseErr := strconv.Atoi(unquote(value))
				if parseErr != nil {
					return config, configLine(path, lineNo, "version must be an integer")
				}
				config.Version, seenVersion = parsed, true
			case "project":
				if value != "" {
					return config, configLine(path, lineNo, key+" must be a mapping")
				}
				section = key
			case "runtime":
				return config, configLine(path, lineNo, "runtime configuration is not supported; configure runtimes in the host platform")
			default:
				return config, configLine(path, lineNo, "unknown top-level key "+key)
			}
			continue
		}
		if indent != 2 || section == "" {
			return config, configLine(path, lineNo, "expected a two-space nested field")
		}
		switch section + "." + key {
		case "project.name":
			config.Name = unquote(value)
		case "project.default_branch":
			config.DefaultBranch = unquote(value)
		default:
			return config, configLine(path, lineNo, "unknown field "+section+"."+key)
		}
	}
	if err := scanner.Err(); err != nil {
		return config, WrapError("config_invalid", "Failed to read project configuration.", err)
	}
	if !seenVersion {
		return config, configLine(path, 0, "version is required")
	}
	if config.Version != 1 {
		return config, configLine(path, 0, fmt.Sprintf("unsupported version %d", config.Version))
	}
	return config, nil
}

func stripComment(value string) string {
	inSingle, inDouble := false, false
	for i, r := range value {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return value[:i]
			}
		}
	}
	return value
}

func unquote(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
		return value[1 : len(value)-1]
	}
	return value
}

func configLine(path string, line int, message string) error {
	where := path
	if line > 0 {
		where = fmt.Sprintf("%s:%d", path, line)
	}
	err := NewError("config_invalid", fmt.Sprintf("Invalid Git Workspace configuration at %s: %s.", where, message))
	err.Details["path"] = path
	if line > 0 {
		err.Details["line"] = line
	}
	return err
}
