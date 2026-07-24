package doctor_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wktbox/internal/config"
	"wktbox/internal/doctor"
	"wktbox/internal/ports"
	"wktbox/internal/process"
)

type scriptedProcessRunner struct {
	worktree   string
	withoutGit bool
	calls      [][]string
}

func (runner *scriptedProcessRunner) Run(
	_ context.Context,
	name string,
	arguments ...string,
) (process.Result, error) {
	call := append([]string{name}, arguments...)
	runner.calls = append(runner.calls, call)
	joined := strings.Join(call, " ")
	switch {
	case runner.withoutGit &&
		strings.Contains(joined, "git ") &&
		strings.Contains(joined, "--show-toplevel"):
		return process.Result{
			ExitCode: 128,
			Stderr:   "fatal: not a git repository",
		}, errors.New("exit status 128")
	case strings.Contains(joined, "git ") && strings.Contains(joined, "--show-toplevel"):
		return process.Result{Stdout: runner.worktree + "\n"}, nil
	case strings.Contains(joined, "git ") && strings.Contains(joined, "--git-common-dir"):
		return process.Result{Stdout: filepath.Join(runner.worktree, ".git") + "\n"}, nil
	case strings.Contains(joined, "git ") && strings.Contains(joined, "--git-dir"):
		return process.Result{Stdout: filepath.Join(runner.worktree, ".git") + "\n"}, nil
	case strings.Contains(joined, "git ") && strings.Contains(joined, "branch --show-current"):
		return process.Result{Stdout: "feature/doctor\n"}, nil
	case strings.Contains(joined, "docker version"):
		return process.Result{Stdout: "29.5.0\n"}, nil
	case strings.Contains(joined, "docker compose version"):
		return process.Result{Stdout: "5.1.0\n"}, nil
	case strings.Contains(joined, "docker info"):
		return process.Result{Stdout: "linux\n"}, nil
	case strings.Contains(joined, "docker run"):
		return process.Result{}, nil
	default:
		return process.Result{}, errors.New("unexpected command: " + joined)
	}
}

func TestHostProbesValidateAvailableHostAndKeepHostGitLimitationVisible(t *testing.T) {
	worktree := t.TempDir()
	runner := &scriptedProcessRunner{worktree: worktree}
	probes := doctor.NewHostProbes(doctor.HostInput{
		Runner:      runner,
		Path:        worktree,
		Environ:     map[string]string{},
		Allocator:   ports.NewAllocator(23000, 10),
		StateRoot:   t.TempDir(),
		DindImage:   "docker:29.5.0-dind",
		MinimumDisk: 1024,
		FreeDisk: func(string) (uint64, error) {
			return 4096, nil
		},
		DindTLS: func(context.Context) error {
			return nil
		},
	})

	report := doctor.Run(context.Background(), doctor.Input{Probes: probes})

	if !report.OK {
		t.Fatalf("report = %#v", report)
	}
	for _, check := range report.Checks {
		if check.Status == doctor.Fail {
			t.Fatalf("failed check = %#v", check)
		}
	}
	bridge := findCheck(t, report, doctor.CheckGitBridge)
	if bridge.Status != doctor.Warn || !strings.Contains(bridge.Message, "git.mode host") {
		t.Fatalf("bridge = %#v", bridge)
	}
}

func TestMountedGitProbeRequiresRuntimeValidation(t *testing.T) {
	worktree := t.TempDir()
	if err := writeConfig(worktree, "mounted"); err != nil {
		t.Fatal(err)
	}
	runner := &scriptedProcessRunner{worktree: worktree}
	probes := doctor.NewHostProbes(doctor.HostInput{
		Runner:    runner,
		Path:      worktree,
		Environ:   map[string]string{},
		Allocator: ports.NewAllocator(23000, 10),
		StateRoot: t.TempDir(),
		FreeDisk: func(string) (uint64, error) {
			return 1 << 40, nil
		},
		GitValidation: func(context.Context) error {
			return errors.New("git status failed")
		},
	})

	result := probes.Check(context.Background(), doctor.CheckGitBridge)

	if result.Status != doctor.Fail ||
		!strings.Contains(result.Message, "git status failed") ||
		!strings.Contains(result.Remediation, "git.mode: host") {
		t.Fatalf("result = %#v", result)
	}
}

func TestHostProbesAcceptPlainWorkspaceWithWarnings(t *testing.T) {
	workspace := t.TempDir()
	if err := writeConfig(workspace, "mounted"); err != nil {
		t.Fatal(err)
	}
	runner := &scriptedProcessRunner{
		worktree:   workspace,
		withoutGit: true,
	}
	probes := doctor.NewHostProbes(doctor.HostInput{
		Runner:      runner,
		Path:        workspace,
		Environ:     map[string]string{},
		Allocator:   ports.NewAllocator(23000, 10),
		StateRoot:   t.TempDir(),
		DindImage:   "docker:29.5.0-dind",
		MinimumDisk: 1024,
		FreeDisk: func(string) (uint64, error) {
			return 4096, nil
		},
	})

	report := doctor.Run(context.Background(), doctor.Input{Probes: probes})

	if !report.OK {
		t.Fatalf("report = %#v", report)
	}
	workspaceCheck := findCheck(t, report, doctor.CheckWorktree)
	if workspaceCheck.Name != "Workspace path" ||
		workspaceCheck.Status != doctor.Warn ||
		!strings.Contains(workspaceCheck.Message, "Workspace") ||
		!strings.Contains(workspaceCheck.Message, "without Git") {
		t.Fatalf("workspace check = %#v", workspaceCheck)
	}
	mountCheck := findCheck(t, report, doctor.CheckBindMount)
	if mountCheck.Name != "Workspace bind mount" ||
		mountCheck.Status != doctor.Pass {
		t.Fatalf("mount check = %#v", mountCheck)
	}
	bridgeCheck := findCheck(t, report, doctor.CheckGitBridge)
	if bridgeCheck.Status != doctor.Warn ||
		!strings.Contains(bridgeCheck.Message, "disabled") {
		t.Fatalf("bridge check = %#v", bridgeCheck)
	}
}

func TestTLSProbeWithoutRunningBoxIsWarningNotFalseSuccess(t *testing.T) {
	worktree := t.TempDir()
	probes := doctor.NewHostProbes(doctor.HostInput{
		Runner:    &scriptedProcessRunner{worktree: worktree},
		Path:      worktree,
		Environ:   map[string]string{},
		Allocator: ports.NewAllocator(23000, 10),
		StateRoot: t.TempDir(),
	})

	result := probes.Check(context.Background(), doctor.CheckDindTLS)

	if result.Status != doctor.Warn || !strings.Contains(result.Message, "running box") {
		t.Fatalf("result = %#v", result)
	}
}

func TestDiskProbeReportsRequiredAndAvailableBytes(t *testing.T) {
	probes := doctor.NewHostProbes(doctor.HostInput{
		StateRoot:   t.TempDir(),
		MinimumDisk: 2 << 30,
		FreeDisk: func(string) (uint64, error) {
			return 512 << 20, nil
		},
	})

	result := probes.Check(context.Background(), doctor.CheckDiskSpace)

	if result.Status != doctor.Fail ||
		!strings.Contains(result.Message, "512 MiB") ||
		!strings.Contains(result.Message, "2048 MiB") {
		t.Fatalf("result = %#v", result)
	}
}

func writeConfig(worktree string, mode config.GitMode) error {
	content := "version: 1\ngit:\n  mode: " + string(mode) + "\n"
	return os.WriteFile(filepath.Join(worktree, ".wktbox.yml"), []byte(content), 0o600)
}

func findCheck(t *testing.T, report doctor.Report, id doctor.CheckID) doctor.Check {
	t.Helper()
	for _, check := range report.Checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("check %s not found", id)
	return doctor.Check{}
}
