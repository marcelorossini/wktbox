package gitbridge

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"wktbox/internal/config"
	"wktbox/internal/discovery"
	"wktbox/internal/process"
)

var ErrUnsafeGitLayout = errors.New("Git administrative directory is outside the common directory")

const commonTarget = "/wktbox/git-common"

type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

type Bridge struct {
	Mounts             []Mount
	Environment        map[string]string
	RequiresValidation bool
	SkippedReason      string
}

type ValidationRunner interface {
	Run(context.Context, []string) (process.Result, error)
}

func Prepare(worktree discovery.Worktree, mode config.GitMode) (Bridge, error) {
	switch mode {
	case config.GitHost:
		return Bridge{}, nil
	case config.GitMounted:
		if !worktree.HasGit() {
			return Bridge{
				SkippedReason: "workspace has no Git metadata",
			}, nil
		}
		if !worktree.IsGitRoot() {
			return Bridge{
				SkippedReason: "selected workspace is below the Git worktree root",
			}, nil
		}
		return prepareMounted(worktree)
	default:
		return Bridge{}, fmt.Errorf("unsupported git mode %q", mode)
	}
}

func Validate(ctx context.Context, runner ValidationRunner) error {
	checks := []struct {
		command       []string
		acceptExitOne bool
	}{
		{command: []string{"git", "status", "--short"}},
		{command: []string{"git", "diff", "--quiet"}, acceptExitOne: true},
		{command: []string{"git", "rev-parse", "--git-common-dir"}},
	}
	for index, check := range checks {
		result, err := runner.Run(ctx, check.command)
		exitAccepted := result.ExitCode == 0 ||
			(check.acceptExitOne && result.ExitCode == 1)
		if err != nil || !exitAccepted {
			return validationError(check.command, result, err)
		}
		if index == len(checks)-1 {
			got := path.Clean(strings.TrimSpace(result.Stdout))
			if got != commonTarget {
				return fmt.Errorf(
					"git.mode: mounted validation returned common dir %q, expected %q; "+
						"use git.mode: host until the mount is compatible",
					got,
					commonTarget,
				)
			}
		}
	}
	return nil
}

func prepareMounted(worktree discovery.Worktree) (Bridge, error) {
	if strings.TrimSpace(worktree.CommonDir) == "" ||
		strings.TrimSpace(worktree.GitDir) == "" {
		return Bridge{}, errors.New("Git common and administrative directories are required")
	}
	relativeGitDir, err := filepath.Rel(worktree.CommonDir, worktree.GitDir)
	if err != nil {
		return Bridge{}, fmt.Errorf("%w: %v", ErrUnsafeGitLayout, err)
	}
	if relativeGitDir == ".." ||
		strings.HasPrefix(relativeGitDir, ".."+string(filepath.Separator)) {
		return Bridge{}, ErrUnsafeGitLayout
	}

	gitDirTarget := commonTarget
	if relativeGitDir != "." {
		gitDirTarget = path.Join(commonTarget, filepath.ToSlash(relativeGitDir))
	}
	return Bridge{
		Mounts: []Mount{{
			Source:   filepath.Clean(worktree.CommonDir),
			Target:   commonTarget,
			ReadOnly: false,
		}},
		Environment: map[string]string{
			"GIT_WORK_TREE":  "/workspace",
			"GIT_DIR":        gitDirTarget,
			"GIT_COMMON_DIR": commonTarget,
		},
		RequiresValidation: true,
	}, nil
}

func validationError(
	command []string,
	result process.Result,
	cause error,
) error {
	detail := strings.TrimSpace(result.Stderr)
	if detail == "" {
		detail = strings.TrimSpace(result.Stdout)
	}
	if detail == "" && cause != nil {
		detail = cause.Error()
	}
	if detail == "" {
		detail = fmt.Sprintf("exit code %d", result.ExitCode)
	}
	return fmt.Errorf(
		"git.mode: mounted validation failed for %q: %s; "+
			"use git.mode: host until the shared metadata mount is compatible",
		strings.Join(command, " "),
		detail,
	)
}
