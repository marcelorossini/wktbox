package discovery_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"wktbox/internal/discovery"
	"wktbox/internal/process"
)

type fakeRunner struct {
	results map[string]process.Result
	errors  map[string]error
}

func (runner fakeRunner) Run(_ context.Context, name string, arguments ...string) (process.Result, error) {
	key := name + "\x00" + strings.Join(arguments, "\x00")
	return runner.results[key], runner.errors[key]
}

func gitKey(path string, arguments ...string) string {
	all := append([]string{"-C", path}, arguments...)
	return "git\x00" + strings.Join(all, "\x00")
}

func TestDiscoverReturnsWorktreeMetadata(t *testing.T) {
	requested := t.TempDir()
	root := filepath.Join(requested, "feature")
	common := filepath.Join(requested, "repository", ".git")
	gitDir := filepath.Join(common, "worktrees", "feature")
	runner := fakeRunner{results: map[string]process.Result{
		gitKey(requested, "rev-parse", "--show-toplevel"): {Stdout: root + "\n"},
		gitKey(root, "rev-parse", "--git-common-dir"):     {Stdout: common + "\n"},
		gitKey(root, "rev-parse", "--git-dir"):            {Stdout: gitDir + "\n"},
		gitKey(root, "branch", "--show-current"):          {Stdout: "feature/auth\n"},
	}}

	got, err := discovery.Discover(context.Background(), runner, requested)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != filepath.Clean(root) {
		t.Fatalf("path = %q", got.Path)
	}
	if got.CommonDir != filepath.Clean(common) {
		t.Fatalf("common dir = %q", got.CommonDir)
	}
	if got.GitDir != filepath.Clean(gitDir) {
		t.Fatalf("git dir = %q", got.GitDir)
	}
	if got.Branch != "feature/auth" {
		t.Fatalf("branch = %q", got.Branch)
	}
	if got.DisplayName != "feature" {
		t.Fatalf("display name = %q", got.DisplayName)
	}
}

func TestDiscoverResolvesRelativeGitDirectoriesFromWorktree(t *testing.T) {
	requested := t.TempDir()
	root := filepath.Join(requested, "feature")
	runner := fakeRunner{results: map[string]process.Result{
		gitKey(requested, "rev-parse", "--show-toplevel"): {Stdout: root + "\n"},
		gitKey(root, "rev-parse", "--git-common-dir"):     {Stdout: "../repository/.git\n"},
		gitKey(root, "rev-parse", "--git-dir"):            {Stdout: "../repository/.git/worktrees/feature\n"},
		gitKey(root, "branch", "--show-current"):          {},
	}}

	got, err := discovery.Discover(context.Background(), runner, requested)
	if err != nil {
		t.Fatal(err)
	}
	wantCommon := filepath.Clean(filepath.Join(root, "../repository/.git"))
	if got.CommonDir != wantCommon {
		t.Fatalf("common dir = %q, want %q", got.CommonDir, wantCommon)
	}
	if got.Branch != "" {
		t.Fatalf("detached branch = %q", got.Branch)
	}
}

func TestDiscoverRejectsDirectoryOutsideGit(t *testing.T) {
	requested := t.TempDir()
	key := gitKey(requested, "rev-parse", "--show-toplevel")
	runner := fakeRunner{
		results: map[string]process.Result{
			key: {ExitCode: 128, Stderr: "fatal: not a git repository"},
		},
		errors: map[string]error{
			key: errors.New("exit status 128"),
		},
	}

	_, err := discovery.Discover(context.Background(), runner, requested)
	if !errors.Is(err, discovery.ErrNotWorktree) {
		t.Fatalf("error = %v", err)
	}
}
