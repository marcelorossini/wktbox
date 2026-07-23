package environment

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"wktbox/internal/config"
)

var (
	ErrInvalidSource = errors.New("invalid project env source")
	ErrInvalidTarget = errors.New("invalid project env target")
	ErrWritableEnv   = errors.New("project env must be read-only")
)

type ProjectEnv struct {
	Source   string
	Target   string
	Mount    bool
	ReadOnly bool
}

func Resolve(
	worktree string,
	cfg config.Config,
	cliFile string,
	cliTarget string,
) (ProjectEnv, error) {
	sourceValue := cliFile
	explicitSource := sourceValue != ""
	if sourceValue == "" {
		sourceValue = cfg.Environment.File
		explicitSource = sourceValue != ""
	}
	if sourceValue == "" {
		fallback := filepath.Join(worktree, ".env")
		if _, err := os.Stat(fallback); err == nil {
			sourceValue = fallback
		} else if !os.IsNotExist(err) {
			return ProjectEnv{}, fmt.Errorf("%w: %v", ErrInvalidSource, err)
		} else {
			return ProjectEnv{}, nil
		}
	}

	target := cliTarget
	if target == "" {
		target = cfg.Environment.Target
	}
	target = path.Clean(target)
	if !validTarget(target) {
		return ProjectEnv{}, fmt.Errorf("%w: %s", ErrInvalidTarget, target)
	}
	if !cfg.Environment.ReadOnly {
		return ProjectEnv{}, ErrWritableEnv
	}

	source := sourceValue
	if !filepath.IsAbs(source) {
		source = filepath.Join(worktree, source)
	}
	source, err := filepath.Abs(source)
	if err != nil {
		return ProjectEnv{}, fmt.Errorf("%w: %v", ErrInvalidSource, err)
	}
	source = filepath.Clean(source)
	info, err := os.Stat(source)
	if err != nil {
		return ProjectEnv{}, fmt.Errorf("%w: %v", ErrInvalidSource, err)
	}
	if !info.Mode().IsRegular() {
		return ProjectEnv{}, fmt.Errorf("%w: %s is not a regular file", ErrInvalidSource, source)
	}

	mount := true
	if containerPath, ok := sourceContainerPath(worktree, cfg.Workspace.Target, source); ok {
		mount = containerPath != target
	}
	if !explicitSource && source == filepath.Join(worktree, ".env") {
		mount = false
	}

	return ProjectEnv{
		Source:   source,
		Target:   target,
		Mount:    mount,
		ReadOnly: true,
	}, nil
}

func validTarget(target string) bool {
	if !path.IsAbs(target) {
		return false
	}
	for _, root := range []string{"/workspace", "/run/wktbox"} {
		if strings.HasPrefix(target, root+"/") {
			return true
		}
	}
	return false
}

func sourceContainerPath(worktree string, workspaceTarget string, source string) (string, bool) {
	absoluteWorktree, err := filepath.Abs(worktree)
	if err != nil {
		return "", false
	}
	relative, err := filepath.Rel(absoluteWorktree, source)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return path.Join(workspaceTarget, filepath.ToSlash(relative)), true
}
