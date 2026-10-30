package gitworkspace

import "context"

type DoctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func (a *App) Doctor(ctx context.Context) (checks []DoctorCheck, err error) {
	err = a.Locker.WithShared(ctx, func(ctx context.Context) error {
		checks = append(checks, DoctorCheck{Name: "git", Status: "ok", Message: "repository: " + a.Repo.Root})
		configPath := a.Paths.ConfigFile()
		config, configErr := LoadConfig(configPath)
		if configErr != nil {
			checks = append(checks, DoctorCheck{Name: "config", Status: "error", Message: configErr.Error()})
		} else {
			message := configPath + " (not created; defaults apply)"
			if config.DefaultBranch != "" {
				message = configPath
			}
			checks = append(checks, DoctorCheck{Name: "config", Status: "ok", Message: message})
		}
		if err := a.Paths.Ensure(); err != nil {
			checks = append(checks, DoctorCheck{Name: "state", Status: "error", Message: err.Error()})
			return nil
		}
		checks = append(checks, DoctorCheck{Name: "state", Status: "ok", Message: a.Paths.StateDir})
		workspaces, listErr := a.Store.Workspaces()
		if listErr != nil {
			checks = append(checks, DoctorCheck{Name: "workspaces", Status: "error", Message: listErr.Error()})
			return nil
		}
		missing := 0
		for _, w := range workspaces {
			if !pathIsDir(w.WorktreePath) {
				missing++
			}
		}
		status, message := "ok", "no Workspaces registered"
		if len(workspaces) > 0 {
			message = intToStr(len(workspaces)) + " Workspace(s) registered"
			if missing > 0 {
				status = "optional"
				message += ", " + intToStr(missing) + " worktree(s) missing (run gws gc --dry-run)"
			}
		}
		checks = append(checks, DoctorCheck{Name: "workspaces", Status: status, Message: message})
		return nil
	})
	return
}

func intToStr(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
