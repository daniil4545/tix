package cli

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/daniil4545/tix/internal/core"
	"github.com/daniil4545/tix/internal/project"
)

// rootDir: TIX_HOME, else ~/tasks.
func rootDir(env Env) (string, error) {
	if dir := env.Getenv("TIX_HOME"); dir != "" {
		return dir, nil
	}
	home := env.Getenv("HOME")
	if home == "" {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return "", fmt.Errorf("home directory unknown, set TIX_HOME: %w", err)
		}
	}
	return filepath.Join(home, "tasks"), nil
}

// OpenStore opens the store: the store, the log (.tix/log, if the folder exists), the store root and
// the project of the working folder (empty - outside a project).
func OpenStore(env Env) (*core.Store, *slog.Logger, string, string, error) {
	root, err := rootDir(env)
	if err != nil {
		return nil, nil, "", "", err
	}
	timeout := defaultLockTimeout
	if raw := env.Getenv("TIX_LOCK_TIMEOUT"); raw != "" {
		if timeout, err = time.ParseDuration(raw); err != nil || timeout <= 0 {
			return nil, nil, "", "", fmt.Errorf("TIX_LOCK_TIMEOUT %q: duration like 5s required", raw)
		}
	}
	log := slog.New(slog.NewTextHandler(&lazyLog{dir: filepath.Join(root, ".tix")}, nil))
	store, err := core.Open(root, core.Options{
		LockTimeout: timeout,
		GitBin:      env.Getenv("TIX_GIT"),
		Log:         log,
	})
	if err != nil {
		return nil, log, root, "", err
	}
	proj, _ := project.Detect(env.Cwd, root, store.Config())
	return store, log, root, proj, nil
}
