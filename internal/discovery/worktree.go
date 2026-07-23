package discovery

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"wktbox/internal/process"
)

var ErrNotWorktree = errors.New("path is not inside a Git worktree")

type Worktree struct {
	Path        string
	CommonDir   string
	GitDir      string
	Branch      string
	DisplayName string
}

func Discover(ctx context.Context, runner process.Runner, requestedPath string) (Worktree, error) {
	if requestedPath == "" {
		requestedPath = "."
	}
	absolutePath, err := filepath.Abs(requestedPath)
	if err != nil {
		return Worktree{}, fmt.Errorf("resolve worktree path: %w", err)
	}
	absolutePath = filepath.Clean(absolutePath)

	rootResult, err := runner.Run(
		ctx,
		"git",
		"-C",
		absolutePath,
		"rev-parse",
		"--show-toplevel",
	)
	if err != nil {
		detail := strings.TrimSpace(rootResult.Stderr)
		if detail == "" {
			detail = err.Error()
		}
		return Worktree{}, fmt.Errorf("%w: %s", ErrNotWorktree, detail)
	}
	root := filepath.Clean(strings.TrimSpace(rootResult.Stdout))
	if root == "." || root == "" {
		return Worktree{}, fmt.Errorf("%w: Git returned an empty worktree path", ErrNotWorktree)
	}

	commonDir, err := gitPath(ctx, runner, root, "--git-common-dir")
	if err != nil {
		return Worktree{}, err
	}
	gitDir, err := gitPath(ctx, runner, root, "--git-dir")
	if err != nil {
		return Worktree{}, err
	}
	branchResult, err := runner.Run(
		ctx,
		"git",
		"-C",
		root,
		"branch",
		"--show-current",
	)
	if err != nil {
		return Worktree{}, fmt.Errorf("discover Git branch: %w", err)
	}

	return Worktree{
		Path:        root,
		CommonDir:   commonDir,
		GitDir:      gitDir,
		Branch:      strings.TrimSpace(branchResult.Stdout),
		DisplayName: filepath.Base(root),
	}, nil
}

func gitPath(
	ctx context.Context,
	runner process.Runner,
	worktree string,
	flag string,
) (string, error) {
	result, err := runner.Run(
		ctx,
		"git",
		"-C",
		worktree,
		"rev-parse",
		flag,
	)
	if err != nil {
		return "", fmt.Errorf("discover %s: %w", flag, err)
	}
	value := strings.TrimSpace(result.Stdout)
	if value == "" {
		return "", fmt.Errorf("discover %s: Git returned an empty path", flag)
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(worktree, value)
	}
	return filepath.Clean(value), nil
}
