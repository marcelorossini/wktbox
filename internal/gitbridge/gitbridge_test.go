package gitbridge_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"wktbox/internal/config"
	"wktbox/internal/discovery"
	"wktbox/internal/gitbridge"
	"wktbox/internal/process"
)

func TestHostModeDoesNotMountSharedGitMetadata(t *testing.T) {
	bridge, err := gitbridge.Prepare(testLinkedWorktree(), config.GitHost)
	if err != nil {
		t.Fatal(err)
	}
	if len(bridge.Mounts) != 0 || len(bridge.Environment) != 0 {
		t.Fatalf("host mode exposed Git metadata: %#v", bridge)
	}
}

func TestMountedModeMapsLinkedWorktreeAdminDirectory(t *testing.T) {
	worktree := testLinkedWorktree()

	bridge, err := gitbridge.Prepare(worktree, config.GitMounted)
	if err != nil {
		t.Fatal(err)
	}

	wantMounts := []gitbridge.Mount{{
		Source:   worktree.CommonDir,
		Target:   "/wktbox/git-common",
		ReadOnly: false,
	}}
	if !reflect.DeepEqual(bridge.Mounts, wantMounts) {
		t.Fatalf("mounts = %#v", bridge.Mounts)
	}
	wantEnvironment := map[string]string{
		"GIT_WORK_TREE":  "/workspace",
		"GIT_DIR":        "/wktbox/git-common/worktrees/feature-auth",
		"GIT_COMMON_DIR": "/wktbox/git-common",
	}
	if !reflect.DeepEqual(bridge.Environment, wantEnvironment) {
		t.Fatalf("environment = %#v", bridge.Environment)
	}
	if !bridge.RequiresValidation {
		t.Fatal("mounted mode did not require validation")
	}
}

func TestMountedModeMapsMainWorktreeToCommonDirectory(t *testing.T) {
	worktree := testLinkedWorktree()
	worktree.GitDir = worktree.CommonDir

	bridge, err := gitbridge.Prepare(worktree, config.GitMounted)
	if err != nil {
		t.Fatal(err)
	}

	if bridge.Environment["GIT_DIR"] != "/wktbox/git-common" {
		t.Fatalf("GIT_DIR = %q", bridge.Environment["GIT_DIR"])
	}
}

func TestMountedModeRejectsGitDirectoryOutsideCommonDirectory(t *testing.T) {
	worktree := testLinkedWorktree()
	worktree.GitDir = filepath.Join(filepath.Dir(worktree.CommonDir), "unrelated", ".git")

	_, err := gitbridge.Prepare(worktree, config.GitMounted)

	if !errors.Is(err, gitbridge.ErrUnsafeGitLayout) {
		t.Fatalf("error = %v", err)
	}
}

func TestMountedModeSkipsWorkspaceWithoutGitMetadata(t *testing.T) {
	bridge, err := gitbridge.Prepare(
		discovery.Worktree{Path: "/workspace"},
		config.GitMounted,
	)
	if err != nil {
		t.Fatal(err)
	}
	if bridge.SkippedReason == "" {
		t.Fatal("expected mounted bridge to be skipped")
	}
	if len(bridge.Mounts) != 0 || len(bridge.Environment) != 0 ||
		bridge.RequiresValidation {
		t.Fatalf("skipped bridge exposed Git metadata: %#v", bridge)
	}
}

func TestMountedModeSkipsSelectedGitSubdirectory(t *testing.T) {
	worktree := testLinkedWorktree()
	worktree.GitRoot = filepath.Dir(worktree.Path)

	bridge, err := gitbridge.Prepare(worktree, config.GitMounted)
	if err != nil {
		t.Fatal(err)
	}
	if bridge.SkippedReason == "" ||
		!strings.Contains(bridge.SkippedReason, "below") {
		t.Fatalf("bridge = %#v", bridge)
	}
	if len(bridge.Mounts) != 0 {
		t.Fatalf("mounts = %#v", bridge.Mounts)
	}
}

type validationRunner struct {
	results []process.Result
	errors  []error
	calls   [][]string
}

func (runner *validationRunner) Run(
	_ context.Context,
	command []string,
) (process.Result, error) {
	runner.calls = append(runner.calls, append([]string(nil), command...))
	index := len(runner.calls) - 1
	return runner.results[index], runner.errors[index]
}

func TestValidateChecksStatusDiffAndCommonDirectory(t *testing.T) {
	runner := &validationRunner{
		results: []process.Result{
			{ExitCode: 0},
			{ExitCode: 1},
			{ExitCode: 0, Stdout: "/wktbox/git-common\n"},
		},
		errors: []error{nil, nil, nil},
	}

	if err := gitbridge.Validate(context.Background(), runner); err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"git", "status", "--short"},
		{"git", "diff", "--quiet"},
		{"git", "rev-parse", "--git-common-dir"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestValidateReturnsActionableErrorWithoutFallingBack(t *testing.T) {
	runner := &validationRunner{
		results: []process.Result{{ExitCode: 128, Stderr: "not a git repository"}},
		errors:  []error{errors.New("exit 128")},
	}

	err := gitbridge.Validate(context.Background(), runner)

	if err == nil ||
		!strings.Contains(err.Error(), "git.mode: mounted") ||
		!strings.Contains(err.Error(), "git.mode: host") {
		t.Fatalf("error = %v", err)
	}
}

func testLinkedWorktree() discovery.Worktree {
	common := filepath.Join("/repo", ".git")
	return discovery.Worktree{
		Path:      filepath.Join("/repo-trees", "feature-auth"),
		GitRoot:   filepath.Join("/repo-trees", "feature-auth"),
		CommonDir: common,
		GitDir:    filepath.Join(common, "worktrees", "feature-auth"),
		Branch:    "feature/auth",
	}
}
