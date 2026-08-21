package gitworkspace

import "context"

type DoctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func (a *App) Doctor(ctx context.Context, selector, cwd string) (checks []DoctorCheck, err error) {
	err = a.Locker.WithShared(ctx, func(ctx context.Context) error {
		project, resolveErr := a.resolveProjectUnlocked(ctx, selector, cwd)
		if resolveErr != nil {
			return resolveErr
		}
		checks = append(checks, DoctorCheck{Name: "git", Status: "ok", Message: "git repository available"})
		if _, configErr := LoadProjectConfig(project.ConfigPath); configErr != nil {
			checks = append(checks, DoctorCheck{Name: "config", Status: "error", Message: configErr.Error()})
			return nil
		}
		checks = append(checks, DoctorCheck{Name: "config", Status: "ok", Message: project.ConfigPath})
		return nil
	})
	return
}
