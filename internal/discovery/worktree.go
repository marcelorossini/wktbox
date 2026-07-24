package discovery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"wktbox/internal/process"
)

var (
	ErrInvalidWorkspace = errors.New("invalid workspace path")
	ErrNotWorktree      = errors.New("path is not inside a Git worktree")
)

type Worktree struct {
	Path          string
	GitRoot       string
	CommonDir     string
	GitDir        string
	Branch        string
	DisplayName   string
	GitProbeError string
}

func (worktree Worktree) HasGit() bool {
	return strings.TrimSpace(worktree.GitRoot) != ""
}

func (worktree Worktree) IsGitRoot() bool {
	return worktree.HasGit() &&
		filepath.Clean(worktree.Path) == filepath.Clean(worktree.GitRoot)
}

func Discover(ctx context.Context, runner process.Runner, requestedPath string) (Worktree, error) {
	if requestedPath == "" {
		requestedPath = "."
	}
	absolutePath, err := filepath.Abs(requestedPath)
	if err != nil {
		return Worktree{}, fmt.Errorf("%w: resolve workspace path: %v", ErrInvalidWorkspace, err)
	}
	resolvedPath, err := filepath.EvalSymlinks(filepath.Clean(absolutePath))
	if err != nil {
		return Worktree{}, fmt.Errorf("%w: resolve workspace path %q: %v", ErrInvalidWorkspace, absolutePath, err)
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return Worktree{}, fmt.Errorf("%w: inspect workspace path %q: %v", ErrInvalidWorkspace, resolvedPath, err)
	}
	if !info.IsDir() {
		return Worktree{}, fmt.Errorf("%w: workspace path %q is not a directory", ErrInvalidWorkspace, resolvedPath)
	}
	resolvedPath = filepath.Clean(resolvedPath)
	workspace := Worktree{
		Path:        resolvedPath,
		DisplayName: filepath.Base(resolvedPath),
	}

	rootResult, err := runner.Run(
		ctx,
		"git",
		"-C",
		resolvedPath,
		"rev-parse",
		"--show-toplevel",
	)
	if err != nil {
		workspace.GitProbeError = probeDiagnostic(rootResult, err)
		return workspace, nil
	}
	root := filepath.Clean(strings.TrimSpace(rootResult.Stdout))
	if root == "." || root == "" {
		workspace.GitProbeError = "Git returned an empty worktree path"
		return workspace, nil
	}

	commonDir, err := gitPath(ctx, runner, root, "--git-common-dir")
	if err != nil {
		workspace.GitProbeError = err.Error()
		return workspace, nil
	}
	gitDir, err := gitPath(ctx, runner, root, "--git-dir")
	if err != nil {
		workspace.GitProbeError = err.Error()
		return workspace, nil
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
		workspace.GitProbeError = fmt.Sprintf("discover Git branch: %v", err)
		return workspace, nil
	}

	workspace.GitRoot = root
	workspace.CommonDir = commonDir
	workspace.GitDir = gitDir
	workspace.Branch = strings.TrimSpace(branchResult.Stdout)
	return workspace, nil
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

func probeDiagnostic(result process.Result, err error) string {
	detail := strings.TrimSpace(result.Stderr)
	if detail != "" {
		return detail
	}
	return err.Error()
}
