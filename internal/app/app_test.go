package app_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"wktbox/internal/app"
	"wktbox/internal/doctor"
	"wktbox/internal/executor"
	"wktbox/internal/identity"
	"wktbox/internal/loopback"
	"wktbox/internal/ports"
	"wktbox/internal/process"
	"wktbox/internal/prune"
	"wktbox/internal/sandbox"
	"wktbox/internal/state"
)

type fakeManager struct {
	ensuredSpec sandbox.Spec
	box         state.BoxRecord
	touched     []string
	loopback    loopback.Status
	syncIDs     []string
	boxes       []state.BoxRecord
	destroyed   []string
	destroyErr  map[string]error
}

func (manager *fakeManager) EnsureAllocated(
	_ context.Context,
	spec sandbox.Spec,
	_ ports.Allocator,
) (state.BoxRecord, error) {
	manager.ensuredSpec = spec
	manager.box = state.BoxRecord{
		ID:                     spec.ID,
		Name:                   spec.Name,
		Branch:                 spec.Branch,
		Worktree:               spec.Worktree,
		ProjectName:            "wktbox-" + spec.ID,
		Status:                 state.Ready,
		Ports:                  ports.Block{Start: 23000, Size: 10},
		ComposePath:            "/state/compose.yml",
		SandboxEnvPath:         "/state/sandbox.env",
		ProjectEnvOverridePath: "/state/env.override.yml",
		GatewayEnabled:         spec.Config.Gateway.Enabled,
	}
	return manager.box, nil
}

func (manager *fakeManager) Inspect(context.Context, string) (state.BoxRecord, error) {
	return manager.box, nil
}

func (manager *fakeManager) List(context.Context) ([]state.BoxRecord, error) {
	if manager.boxes != nil {
		return manager.boxes, nil
	}
	return []state.BoxRecord{manager.box}, nil
}

func (manager *fakeManager) Stop(context.Context, string) error {
	return nil
}

func (manager *fakeManager) Restart(context.Context, string) error {
	return nil
}

func (manager *fakeManager) Destroy(_ context.Context, id string) error {
	manager.destroyed = append(manager.destroyed, id)
	return manager.destroyErr[id]
}

func (manager *fakeManager) Touch(_ context.Context, id string) error {
	manager.touched = append(manager.touched, id)
	return nil
}

func (manager *fakeManager) SyncLoopback(
	_ context.Context,
	id string,
) (loopback.Status, error) {
	manager.syncIDs = append(manager.syncIDs, id)
	return manager.loopback, nil
}

type discoveryRunner struct {
	worktree string
}

func (runner discoveryRunner) Run(
	_ context.Context,
	_ string,
	arguments ...string,
) (process.Result, error) {
	joined := strings.Join(arguments, " ")
	switch {
	case strings.Contains(joined, "--show-toplevel"):
		return process.Result{Stdout: runner.worktree + "\n"}, nil
	case strings.Contains(joined, "--git-common-dir"):
		return process.Result{Stdout: runner.worktree + "/.git\n"}, nil
	case strings.Contains(joined, "--git-dir"):
		return process.Result{Stdout: runner.worktree + "/.git/worktrees/feature\n"}, nil
	case strings.Contains(joined, "branch --show-current"):
		return process.Result{Stdout: "feature/auth\n"}, nil
	default:
		return process.Result{}, nil
	}
}

type interactiveRunner struct {
	invocations []executor.Invocation
	code        int
	codes       []int
	stdout      []string
}

func (runner *interactiveRunner) Run(
	_ context.Context,
	invocation executor.Invocation,
) (int, error) {
	runner.invocations = append(runner.invocations, invocation)
	index := len(runner.invocations) - 1
	code := runner.code
	if index < len(runner.codes) {
		code = runner.codes[index]
	}
	if index < len(runner.stdout) && invocation.Stdout != nil {
		_, _ = io.WriteString(invocation.Stdout, runner.stdout[index])
	}
	return code, nil
}

func TestResolveAndEnsureBuildSandboxSpecFromWorktree(t *testing.T) {
	worktree := t.TempDir()
	manager := &fakeManager{}
	interactive := &interactiveRunner{}
	service := app.New(app.Options{
		ProcessRunner:     discoveryRunner{worktree: worktree},
		Manager:           manager,
		InteractiveRunner: interactive,
		Allocator:         ports.NewAllocator(23000, 10),
		Environ:           map[string]string{},
		Version:           "test",
		Timezone:          "America/Sao_Paulo",
		PUID:              1000,
		PGID:              1000,
	})

	resolution, err := service.Resolve(context.Background(), app.Request{Path: worktree})
	if err != nil {
		t.Fatal(err)
	}
	box, err := service.Ensure(context.Background(), resolution)
	if err != nil {
		t.Fatal(err)
	}

	if resolution.ID == "" || len(resolution.ID) != 12 {
		t.Fatalf("ID = %q", resolution.ID)
	}
	if box.ID != resolution.ID {
		t.Fatalf("box ID = %q, resolution ID = %q", box.ID, resolution.ID)
	}
	if manager.ensuredSpec.Worktree != worktree ||
		manager.ensuredSpec.Branch != "feature/auth" ||
		manager.ensuredSpec.Version != "test" ||
		manager.ensuredSpec.Timezone != "America/Sao_Paulo" {
		t.Fatalf("spec = %#v", manager.ensuredSpec)
	}
}

func TestResolveUsesReleaseVersionForRuntimeImages(t *testing.T) {
	worktree := t.TempDir()
	service := app.New(app.Options{
		ProcessRunner:     discoveryRunner{worktree: worktree},
		Manager:           &fakeManager{},
		InteractiveRunner: &interactiveRunner{},
		Allocator:         ports.NewAllocator(23000, 10),
		Environ:           map[string]string{},
		Version:           "0.1.0",
	})

	resolution, err := service.Resolve(
		context.Background(),
		app.Request{Path: worktree},
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Spec.Config.Runtime.WebtopImage !=
		"ghcr.io/marcelorossini/wktbox/webtop:0.1.0" {
		t.Fatalf(
			"webtop image = %q",
			resolution.Spec.Config.Runtime.WebtopImage,
		)
	}
	if resolution.Spec.Config.Runtime.GatewayImage !=
		"ghcr.io/marcelorossini/wktbox/gateway:0.1.0" {
		t.Fatalf(
			"gateway image = %q",
			resolution.Spec.Config.Runtime.GatewayImage,
		)
	}
}

func TestRunUsesExecutorAndTouchesBoxAfterChildExit(t *testing.T) {
	manager := &fakeManager{}
	interactive := &interactiveRunner{code: 17}
	service := app.New(app.Options{
		Manager:           manager,
		InteractiveRunner: interactive,
		Allocator:         ports.NewAllocator(23000, 10),
	})
	box := state.BoxRecord{
		ID:             "a4f8c9137d2b",
		Worktree:       "/repo",
		ProjectName:    "wktbox-a4f8c9137d2b",
		ComposePath:    "/state/compose.yml",
		SandboxEnvPath: "/state/sandbox.env",
	}

	code, err := service.Run(
		context.Background(),
		box,
		[]string{"printf", "%s", "value with spaces"},
		executor.Options{HostCWD: "/repo"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if code != 17 {
		t.Fatalf("code = %d", code)
	}
	if len(interactive.invocations) != 1 {
		t.Fatalf("invocations = %d", len(interactive.invocations))
	}
	gotArgs := interactive.invocations[0].Arguments
	wantSuffix := []string{"webtop", "printf", "%s", "value with spaces"}
	if !reflect.DeepEqual(gotArgs[len(gotArgs)-len(wantSuffix):], wantSuffix) {
		t.Fatalf("arguments = %#v", gotArgs)
	}
	if !reflect.DeepEqual(manager.touched, []string{box.ID}) {
		t.Fatalf("touches = %#v", manager.touched)
	}
}

func TestSyncLoopbackDelegatesToManager(t *testing.T) {
	want := loopback.Status{EventStream: loopback.EventStreamConnected}
	manager := &fakeManager{loopback: want}
	service := app.New(app.Options{Manager: manager})
	box := state.BoxRecord{ID: "a4f8c9137d2b"}

	got, err := service.SyncLoopback(context.Background(), box)

	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) ||
		!reflect.DeepEqual(manager.syncIDs, []string{box.ID}) {
		t.Fatalf("status=%#v sync IDs=%#v", got, manager.syncIDs)
	}
}

func TestPruneDryRunDiscoversMissingWorktreesWithoutDestroying(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "removed")
	manager := &fakeManager{boxes: []state.BoxRecord{{
		ID:       "aaaaaaaaaaaa",
		Name:     "removed",
		Worktree: missing,
	}}}
	service := app.New(app.Options{Manager: manager})

	report, err := service.Prune(context.Background(), false)

	if err != nil {
		t.Fatal(err)
	}
	want := []prune.Candidate{{
		ID:       "aaaaaaaaaaaa",
		Name:     "removed",
		Worktree: missing,
	}}
	if !reflect.DeepEqual(report.Candidates, want) {
		t.Fatalf("candidates = %#v, want %#v", report.Candidates, want)
	}
	if len(report.Destroyed) != 0 {
		t.Fatalf("destroyed report = %#v", report.Destroyed)
	}
	if len(manager.destroyed) != 0 {
		t.Fatalf("destroy calls = %#v", manager.destroyed)
	}
}

func TestPruneForceDestroysOnlyMissingWorktrees(t *testing.T) {
	existing := t.TempDir()
	missing := filepath.Join(t.TempDir(), "removed")
	manager := &fakeManager{boxes: []state.BoxRecord{
		{ID: "bbbbbbbbbbbb", Name: "existing", Worktree: existing},
		{ID: "aaaaaaaaaaaa", Name: "removed", Worktree: missing},
	}}
	service := app.New(app.Options{Manager: manager})

	report, err := service.Prune(context.Background(), true)

	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(manager.destroyed, []string{"aaaaaaaaaaaa"}) {
		t.Fatalf("destroy calls = %#v", manager.destroyed)
	}
	if !reflect.DeepEqual(report.Destroyed, report.Candidates) {
		t.Fatalf(
			"destroyed = %#v, candidates = %#v",
			report.Destroyed,
			report.Candidates,
		)
	}
}

func TestPruneReturnsPartialReportWhenDestroyFails(t *testing.T) {
	manager := &fakeManager{
		boxes: []state.BoxRecord{
			{
				ID:       "bbbbbbbbbbbb",
				Name:     "second",
				Worktree: filepath.Join(t.TempDir(), "second"),
			},
			{
				ID:       "aaaaaaaaaaaa",
				Name:     "first",
				Worktree: filepath.Join(t.TempDir(), "first"),
			},
		},
		destroyErr: map[string]error{
			"bbbbbbbbbbbb": errors.New("compose down failed"),
		},
	}
	service := app.New(app.Options{Manager: manager})

	report, err := service.Prune(context.Background(), true)

	if err == nil ||
		!strings.Contains(err.Error(), "bbbbbbbbbbbb") ||
		!strings.Contains(err.Error(), "compose down failed") {
		t.Fatalf("error = %v", err)
	}
	if !reflect.DeepEqual(manager.destroyed, []string{
		"aaaaaaaaaaaa",
		"bbbbbbbbbbbb",
	}) {
		t.Fatalf("destroy calls = %#v", manager.destroyed)
	}
	if len(report.Destroyed) != 1 ||
		report.Destroyed[0].ID != "aaaaaaaaaaaa" {
		t.Fatalf("partial report = %#v", report)
	}
}

func TestMountedGitModeIsRenderedAndValidatedInsideWebtop(t *testing.T) {
	worktree := t.TempDir()
	if err := os.WriteFile(
		worktree+"/.wktbox.yml",
		[]byte("version: 1\ngit:\n  mode: mounted\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	manager := &fakeManager{}
	interactive := &interactiveRunner{
		codes:  []int{0, 1, 0},
		stdout: []string{"", "", "/wktbox/git-common\n"},
	}
	service := app.New(app.Options{
		ProcessRunner:     discoveryRunner{worktree: worktree},
		Manager:           manager,
		InteractiveRunner: interactive,
		Allocator:         ports.NewAllocator(23000, 10),
		Environ:           map[string]string{},
		Version:           "test",
		Timezone:          "UTC",
	})

	resolution, err := service.Resolve(context.Background(), app.Request{Path: worktree})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Ensure(context.Background(), resolution); err != nil {
		t.Fatal(err)
	}

	if !manager.ensuredSpec.GitBridge.RequiresValidation ||
		len(manager.ensuredSpec.GitBridge.Mounts) != 1 {
		t.Fatalf("bridge = %#v", manager.ensuredSpec.GitBridge)
	}
	gotCommands := make([][]string, 0, len(interactive.invocations))
	for _, invocation := range interactive.invocations {
		arguments := invocation.Arguments
		webtopIndex := -1
		for index, argument := range arguments {
			if argument == "webtop" {
				webtopIndex = index
			}
		}
		gotCommands = append(gotCommands, arguments[webtopIndex+1:])
	}
	wantCommands := [][]string{
		{"git", "status", "--short"},
		{"git", "diff", "--quiet"},
		{"git", "rev-parse", "--git-common-dir"},
	}
	if !reflect.DeepEqual(gotCommands, wantCommands) {
		t.Fatalf("commands = %#v", gotCommands)
	}
}

func TestMountedGitValidationFailureDoesNotSilentlyFallBackToHost(t *testing.T) {
	worktree := t.TempDir()
	if err := os.WriteFile(
		worktree+"/.wktbox.yml",
		[]byte("version: 1\ngit:\n  mode: mounted\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	manager := &fakeManager{}
	interactive := &interactiveRunner{codes: []int{128}}
	service := app.New(app.Options{
		ProcessRunner:     discoveryRunner{worktree: worktree},
		Manager:           manager,
		InteractiveRunner: interactive,
		Allocator:         ports.NewAllocator(23000, 10),
		Environ:           map[string]string{},
	})
	resolution, err := service.Resolve(context.Background(), app.Request{Path: worktree})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.Ensure(context.Background(), resolution)

	if err == nil ||
		!strings.Contains(err.Error(), "git.mode: mounted") ||
		!strings.Contains(err.Error(), "git.mode: host") {
		t.Fatalf("error = %v", err)
	}
	if !manager.ensuredSpec.GitBridge.RequiresValidation {
		t.Fatal("manager received a silent host-mode fallback")
	}
}

func TestLogsStreamsDockerComposeArgumentsWithoutShell(t *testing.T) {
	manager := &fakeManager{}
	interactive := &interactiveRunner{}
	service := app.New(app.Options{
		Manager:           manager,
		InteractiveRunner: interactive,
		Allocator:         ports.NewAllocator(23000, 10),
	})
	box := state.BoxRecord{
		ID:                     "a4f8c9137d2b",
		ProjectName:            "wktbox-a4f8c9137d2b",
		ComposePath:            "/state/compose.yml",
		ProjectEnvOverridePath: "/state/env.override.yml",
		SandboxEnvPath:         "/state/sandbox.env",
		GatewayEnabled:         true,
	}

	code, err := service.Logs(
		context.Background(),
		box,
		"docker",
		true,
		io.Discard,
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if len(interactive.invocations) != 1 {
		t.Fatalf("invocations = %d", len(interactive.invocations))
	}
	got := interactive.invocations[0]
	if got.Name != "docker" {
		t.Fatalf("name = %q", got.Name)
	}
	want := []string{
		"compose", "-p", box.ProjectName,
		"--env-file", box.SandboxEnvPath,
		"-f", box.ComposePath,
		"-f", box.ProjectEnvOverridePath,
		"--profile", "gateway",
		"logs", "--follow", "docker",
	}
	if !reflect.DeepEqual(got.Arguments, want) {
		t.Fatalf("arguments = %#v, want %#v", got.Arguments, want)
	}
}

func TestResolutionCarriesFlagOverridesWithoutReadingCommandEnvironment(t *testing.T) {
	worktree := t.TempDir()
	service := app.New(app.Options{
		ProcessRunner:     discoveryRunner{worktree: worktree},
		Manager:           &fakeManager{},
		InteractiveRunner: &interactiveRunner{},
		Allocator:         ports.NewAllocator(23000, 10),
		Environ:           map[string]string{},
		Version:           "test",
		Timezone:          "UTC",
	})

	_, err := service.Resolve(context.Background(), app.Request{
		Path:       worktree,
		ConfigPath: "missing.yml",
		Environment: []string{
			"WKTBOX_ENV_FILE=must-not-affect-config.env",
		},
	})
	if err == nil {
		t.Fatal("expected explicit missing config error")
	}
	if !strings.Contains(err.Error(), "missing.yml") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveRejectsNamedProfileUntilSchemaSupportsIt(t *testing.T) {
	service := app.New(app.Options{
		ProcessRunner:     discoveryRunner{worktree: t.TempDir()},
		Manager:           &fakeManager{},
		InteractiveRunner: &interactiveRunner{},
		Allocator:         ports.NewAllocator(23000, 10),
		Environ:           map[string]string{},
	})

	_, err := service.Resolve(context.Background(), app.Request{
		Path:    ".",
		Profile: "e2e",
	})

	if err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateWorktreePathRejectsWindowsUNCAndAllowsSpaces(t *testing.T) {
	if err := app.ValidateWorktreePath(
		`C:\repo trees\feature auth`,
		identity.Windows,
	); err != nil {
		t.Fatalf("local path with spaces: %v", err)
	}
	err := app.ValidateWorktreePath(
		`\\server\share\feature`,
		identity.Windows,
	)
	if err == nil || !strings.Contains(err.Error(), "UNC") {
		t.Fatalf("UNC error = %v", err)
	}
}

func TestDoctorReturnsEveryCheckEvenWhenDockerProbesFail(t *testing.T) {
	worktree := t.TempDir()
	service := app.New(app.Options{
		ProcessRunner:     discoveryRunner{worktree: worktree},
		Manager:           &fakeManager{},
		InteractiveRunner: &interactiveRunner{},
		Allocator:         ports.NewAllocator(23000, 10),
		Environ:           map[string]string{},
		StateRoot:         t.TempDir(),
		DoctorFreeDisk: func(string) (uint64, error) {
			return 1 << 40, nil
		},
	})

	value, err := service.Doctor(context.Background(), app.Request{Path: worktree})
	if err != nil {
		t.Fatal(err)
	}
	report, ok := value.(doctor.Report)
	if !ok {
		t.Fatalf("report type = %T", value)
	}
	if len(report.Checks) != doctor.RequiredCheckCount {
		t.Fatalf("checks = %d", len(report.Checks))
	}
	if report.OK {
		t.Fatal("empty Docker version probes should fail")
	}
}
