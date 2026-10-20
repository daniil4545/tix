package cli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"tracker/internal/core"
	"tracker/internal/project"
)

// rootDir: TRACKER_HOME, иначе ~/tasks.
func rootDir(env Env) (string, error) {
	if dir := env.Getenv("TRACKER_HOME"); dir != "" {
		return dir, nil
	}
	home := env.Getenv("HOME")
	if home == "" {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return "", fmt.Errorf("home directory unknown, set TRACKER_HOME: %w", err)
		}
	}
	return filepath.Join(home, "tasks"), nil
}

// OpenStore открывает базу: база, лог (.tracker/log, если папка уже есть), корень базы и
// проект рабочей папки (пусто - вне проекта).
func OpenStore(env Env) (*core.Store, *slog.Logger, string, string, error) {
	root, err := rootDir(env)
	if err != nil {
		return nil, nil, "", "", err
	}
	timeout := defaultLockTimeout
	if raw := env.Getenv("TRACKER_LOCK_TIMEOUT"); raw != "" {
		if timeout, err = time.ParseDuration(raw); err != nil || timeout <= 0 {
			return nil, nil, "", "", fmt.Errorf("TRACKER_LOCK_TIMEOUT %q: duration like 5s required", raw)
		}
	}
	log := slog.New(slog.NewTextHandler(&lazyLog{dir: filepath.Join(root, ".tracker")}, nil))
	store, err := core.Open(root, core.Options{
		LockTimeout: timeout,
		GitBin:      env.Getenv("TRACKER_GIT"),
		Log:         log,
	})
	if err != nil {
		return nil, log, root, "", err
	}
	proj, _ := project.Detect(env.Cwd, root, store.Config())
	return store, log, root, proj, nil
}
