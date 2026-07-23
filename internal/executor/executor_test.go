package executor_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"wktbox/internal/executor"
	"wktbox/internal/state"
)

type recordingInteractiveRunner struct {
	invocations []executor.Invocation
	exitCode    int
	err         error
}

func (runner *recordingInteractiveRunner) Run(
	_ context.Context,
	invocation executor.Invocation,
) (int, error) {
	runner.invocations = append(runner.invocations, invocation)
	return runner.exitCode, runner.err
}

func TestMapWorkingDirectoryPreservesRelativeSubdirectory(t *testing.T) {
	worktree := t.TempDir()
	hostCWD := filepath.Join(worktree, "frontend", "src")

	got, err := executor.MapWorkingDirectory(worktree, hostCWD)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/workspace/frontend/src" {
		t.Fatalf("cwd = %q", got)
	}
}

func TestMapWorkingDirectoryUsesWorkspaceWhenHostIsOutside(t *testing.T) {
	got, err := executor.MapWorkingDirectory(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != "/workspace" {
		t.Fatalf("cwd = %q", got)
	}
}

func TestRunForwardsArgumentsWithoutShellReconstruction(t *testing.T) {
	runner := &recordingInteractiveRunner{}
	commandExecutor := executor.New(runner)
	var stdin bytes.Buffer
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode, err := commandExecutor.Run(
		context.Background(),
		testBox(),
		[]string{"printf", "%s", "a b", "$(must-not-expand)"},
		executor.Options{
			HostCWD:     "/repo/feature/frontend",
			TTY:         false,
			Environment: []string{"CI=true", "DEBUG=e2e"},
			Stdin:       &stdin,
			Stdout:      &stdout,
			Stderr:      &stderr,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if exitCode != 0 {
		t.Fatalf("exit code = %d", exitCode)
	}
	want := []string{
		"compose",
		"-p", "wktbox-a4f8c9137d2b",
		"--env-file", "/state/a/sandbox.env",
		"-f", "/state/a/compose.yml",
		"-f", "/state/a/project-env.override.yml",
		"--profile", "gateway",
		"exec",
		"-T",
		"-w", "/workspace/frontend",
		"--env", "CI=true",
		"--env", "DEBUG=e2e",
		"webtop",
		"printf", "%s", "a b", "$(must-not-expand)",
	}
	got := runner.invocations[0]
	if got.Name != "docker" || !reflect.DeepEqual(got.Arguments, want) {
		t.Fatalf("invocation = %#v\nwant args = %#v", got, want)
	}
	if got.Stdin != &stdin || got.Stdout != &stdout || got.Stderr != &stderr {
		t.Fatal("standard streams were not forwarded")
	}
}

func TestRunAllocatesTTYByOmittingDisableFlag(t *testing.T) {
	runner := &recordingInteractiveRunner{}
	_, err := executor.New(runner).Run(
		context.Background(),
		testBox(),
		[]string{"bash"},
		executor.Options{HostCWD: "/repo/feature", TTY: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, argument := range runner.invocations[0].Arguments {
		if argument == "-T" {
			t.Fatal("interactive execution disabled the TTY")
		}
	}
}

func TestRunReturnsChildExitCode(t *testing.T) {
	runner := &recordingInteractiveRunner{exitCode: 42}
	exitCode, err := executor.New(runner).Run(
		context.Background(),
		testBox(),
		[]string{"false"},
		executor.Options{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if exitCode != 42 {
		t.Fatalf("exit code = %d", exitCode)
	}
}

func TestRunRejectsEmptyCommand(t *testing.T) {
	_, err := executor.New(&recordingInteractiveRunner{}).Run(
		context.Background(),
		testBox(),
		nil,
		executor.Options{},
	)
	if err == nil {
		t.Fatal("expected empty command error")
	}
}

func TestInvalidEnvironmentErrorDoesNotRevealAssignmentValue(t *testing.T) {
	_, err := executor.New(&recordingInteractiveRunner{}).Run(
		context.Background(),
		testBox(),
		[]string{"true"},
		executor.Options{
			Environment: []string{"INVALID-KEY=do-not-leak"},
		},
	)
	if err == nil {
		t.Fatal("expected invalid environment error")
	}
	if strings.Contains(err.Error(), "do-not-leak") {
		t.Fatalf("error leaked environment value: %v", err)
	}
}

func TestOSInteractiveRunnerReturnsNonzeroExitWithoutInfrastructureError(t *testing.T) {
	runner := executor.OSInteractiveRunner{}
	exitCode, err := runner.Run(context.Background(), executor.Invocation{
		Name: os.Args[0],
		Arguments: []string{
			"-test.run=TestExecutorProcessHelper",
			"--",
			"exit-23",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if exitCode != 23 {
		t.Fatalf("exit code = %d", exitCode)
	}
}

func TestExecutorProcessHelper(t *testing.T) {
	for _, argument := range os.Args {
		if argument == "exit-23" {
			os.Exit(23)
		}
	}
}

func testBox() state.BoxRecord {
	return state.BoxRecord{
		ID:                     "a4f8c9137d2b",
		Worktree:               "/repo/feature",
		ProjectName:            "wktbox-a4f8c9137d2b",
		Status:                 state.Ready,
		ComposePath:            "/state/a/compose.yml",
		SandboxEnvPath:         "/state/a/sandbox.env",
		ProjectEnvOverridePath: "/state/a/project-env.override.yml",
		GatewayEnabled:         true,
	}
}
