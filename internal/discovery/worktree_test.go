package discovery_test

import (
	"context"
	"errors"
	"os"
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
	root := requested
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
	if got.GitRoot != filepath.Clean(root) {
		t.Fatalf("git root = %q", got.GitRoot)
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
	if got.DisplayName != filepath.Base(root) {
		t.Fatalf("display name = %q", got.DisplayName)
	}
	if !got.HasGit() {
		t.Fatal("expected Git metadata")
	}
	if !got.IsGitRoot() {
		t.Fatal("expected selected workspace to be the Git root")
	}
}

func TestDiscoverResolvesRelativeGitDirectoriesFromWorktree(t *testing.T) {
	requested := t.TempDir()
	root := requested
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

func TestDiscoverAcceptsDirectoryOutsideGit(t *testing.T) {
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

	got, err := discovery.Discover(context.Background(), runner, requested)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != filepath.Clean(requested) {
		t.Fatalf("path = %q", got.Path)
	}
	if got.DisplayName != filepath.Base(requested) {
		t.Fatalf("display name = %q", got.DisplayName)
	}
	if got.HasGit() {
		t.Fatal("plain workspace should not have Git metadata")
	}
	if got.GitProbeError == "" {
		t.Fatal("expected Git probe diagnostic")
	}
}

func TestDiscoverPreservesSubdirectoryInsideGitRepository(t *testing.T) {
	root := t.TempDir()
	requested := filepath.Join(root, "services", "api")
	if err := os.MkdirAll(requested, 0o755); err != nil {
		t.Fatal(err)
	}
	common := filepath.Join(root, ".git")
	runner := fakeRunner{results: map[string]process.Result{
		gitKey(requested, "rev-parse", "--show-toplevel"): {Stdout: root + "\n"},
		gitKey(root, "rev-parse", "--git-common-dir"):     {Stdout: common + "\n"},
		gitKey(root, "rev-parse", "--git-dir"):            {Stdout: common + "\n"},
		gitKey(root, "branch", "--show-current"):          {Stdout: "main\n"},
	}}

	got, err := discovery.Discover(context.Background(), runner, requested)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != filepath.Clean(requested) {
		t.Fatalf("path = %q", got.Path)
	}
	if got.GitRoot != filepath.Clean(root) {
		t.Fatalf("git root = %q", got.GitRoot)
	}
	if !got.HasGit() {
		t.Fatal("expected Git metadata")
	}
	if got.IsGitRoot() {
		t.Fatal("subdirectory should not be treated as the Git root")
	}
}

func TestDiscoverRejectsMissingPath(t *testing.T) {
	requested := filepath.Join(t.TempDir(), "missing")

	_, err := discovery.Discover(context.Background(), fakeRunner{}, requested)
	if !errors.Is(err, discovery.ErrInvalidWorkspace) {
		t.Fatalf("error = %v", err)
	}
}

func TestDiscoverRejectsRegularFile(t *testing.T) {
	requested := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(requested, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := discovery.Discover(context.Background(), fakeRunner{}, requested)
	if !errors.Is(err, discovery.ErrInvalidWorkspace) {
		t.Fatalf("error = %v", err)
	}
}
