package gitworkspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Store struct{ Paths Paths }

func atomicJSON(path string, value any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmpName, path); err != nil {
		return err
	}
	if d, openErr := os.Open(dir); openErr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func readJSON[T any](path string) (T, error) {
	var value T
	data, err := os.ReadFile(path)
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return value, fmt.Errorf("decode %s: %w", path, err)
	}
	return value, nil
}

func listJSON[T any](dir string) ([]T, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []T{}, nil
	}
	if err != nil {
		return nil, err
	}
	values := make([]T, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		value, err := readJSON[T](filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func (s Store) SaveWorkspace(value Workspace) error {
	value.StateVersion = CurrentStateVersion
	return atomicJSON(s.Paths.WorkspaceFile(value.ID), value)
}
func (s Store) Workspace(id string) (Workspace, error) {
	value, err := readJSON[Workspace](s.Paths.WorkspaceFile(id))
	if err != nil {
		return Workspace{}, err
	}
	return normalizeWorkspaceState(value)
}
func (s Store) Workspaces() ([]Workspace, error) {
	values, err := listJSON[Workspace](s.Paths.WorkspacesDir())
	if err != nil {
		return nil, err
	}
	for i := range values {
		values[i], err = normalizeWorkspaceState(values[i])
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Name != values[j].Name {
			return values[i].Name < values[j].Name
		}
		return values[i].ID < values[j].ID
	})
	return values, nil
}
func (s Store) DeleteWorkspace(id string) error { return removeIfExists(s.Paths.WorkspaceFile(id)) }

func removeIfExists(path string) error {
	err := os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func normalizeWorkspaceState(value Workspace) (Workspace, error) {
	if value.StateVersion == 0 {
		value.StateVersion = CurrentStateVersion
	}
	if value.StateVersion != CurrentStateVersion {
		return Workspace{}, NewError("state_version_unsupported", fmt.Sprintf("Workspace %q uses unsupported state version %d; this gws release supports version %d.", value.ID, value.StateVersion, CurrentStateVersion))
	}
	return value, nil
}
