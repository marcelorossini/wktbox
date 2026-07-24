package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"wktbox/internal/agentintegration"
	"wktbox/internal/app"
	"wktbox/internal/cli"
	"wktbox/internal/executor"
	"wktbox/internal/loopback"
	"wktbox/internal/ports"
	"wktbox/internal/prune"
	"wktbox/internal/state"
)

type fakeService struct {
	calls      []string
	status     state.Status
	runCode    int
	runErr     error
	box        state.BoxRecord
	list       []state.BoxRecord
	logOutput  string
	doctorData any
	syncStatus loopback.Status
	syncErr    error
	childOut   string
	pruneData  prune.Report
	pruneErr   error
}

type fakeAgentManager struct {
	calls  []string
	report agentintegration.Report
	err    error
}

func (fake *fakeAgentManager) Install(
	_ context.Context,
	options agentintegration.Options,
) (agentintegration.Report, error) {
	fake.calls = append(fake.calls, agentCall("install", options))
	return fake.report, fake.err
}

func (fake *fakeAgentManager) Status(
	_ context.Context,
	options agentintegration.Options,
) (agentintegration.Report, error) {
	fake.calls = append(fake.calls, agentCall("status", options))
	return fake.report, fake.err
}

func (fake *fakeAgentManager) Uninstall(
	_ context.Context,
	options agentintegration.Options,
) (agentintegration.Report, error) {
	fake.calls = append(fake.calls, agentCall("uninstall", options))
	return fake.report, fake.err
}

func newFakeService() *fakeService {
	box := state.BoxRecord{
		ID:             "a4f8c9137d2b",
		Name:           "feature-auth",
		Worktree:       "/repo",
		ProjectName:    "wktbox-a4f8c9137d2b",
		Status:         state.Ready,
		Ports:          ports.Block{Start: 23000, Size: 10},
		ComposePath:    "/state/compose.yml",
		SandboxEnvPath: "/state/sandbox.env",
	}
	return &fakeService{
		status:     state.Ready,
		box:        box,
		list:       []state.BoxRecord{box},
		doctorData: map[string]any{"ok": true},
		syncStatus: loopback.Status{
			EventStream: loopback.EventStreamConnected,
			Routes:      []loopback.Route{},
			Warnings:    []loopback.Warning{},
		},
	}
}

func (fake *fakeService) Resolve(_ context.Context, request app.Request) (app.Resolution, error) {
	fake.calls = append(fake.calls, "Resolve:"+request.Path)
	fake.box.Worktree = request.Path
	return app.Resolution{Request: request, ID: fake.box.ID}, nil
}

func (fake *fakeService) Ensure(_ context.Context, _ app.Resolution) (state.BoxRecord, error) {
	fake.calls = append(fake.calls, "Ensure")
	fake.box.Status = state.Ready
	return fake.box, nil
}

func (fake *fakeService) Inspect(_ context.Context, _ app.Resolution) (state.BoxRecord, error) {
	fake.calls = append(fake.calls, "Inspect")
	fake.box.Status = fake.status
	return fake.box, nil
}

func (fake *fakeService) List(context.Context) ([]state.BoxRecord, error) {
	fake.calls = append(fake.calls, "List")
	return fake.list, nil
}

func (fake *fakeService) Prune(
	_ context.Context,
	force bool,
) (prune.Report, error) {
	fake.calls = append(fake.calls, "Prune:"+boolString(force))
	return fake.pruneData, fake.pruneErr
}

func (fake *fakeService) Run(
	_ context.Context,
	_ state.BoxRecord,
	command []string,
	options executor.Options,
) (int, error) {
	fake.calls = append(fake.calls, "Run:"+strings.Join(command, "|"))
	fake.calls = append(fake.calls, "Env:"+strings.Join(options.Environment, "|"))
	if options.Stdout != nil {
		_, _ = io.WriteString(options.Stdout, fake.childOut)
	}
	return fake.runCode, fake.runErr
}

func (fake *fakeService) SyncLoopback(
	context.Context,
	state.BoxRecord,
) (loopback.Status, error) {
	fake.calls = append(fake.calls, "SyncLoopback")
	return fake.syncStatus, fake.syncErr
}

func (fake *fakeService) Logs(
	_ context.Context,
	_ state.BoxRecord,
	service string,
	follow bool,
	stdout io.Writer,
	_ io.Writer,
) (int, error) {
	fake.calls = append(fake.calls, "Logs:"+service+":"+boolString(follow))
	_, _ = io.WriteString(stdout, fake.logOutput)
	return 0, nil
}

func (fake *fakeService) Stop(_ context.Context, _ app.Resolution) error {
	fake.calls = append(fake.calls, "Stop")
	return nil
}

func (fake *fakeService) Restart(_ context.Context, _ app.Resolution) error {
	fake.calls = append(fake.calls, "Restart")
	return nil
}

func (fake *fakeService) Destroy(_ context.Context, _ app.Resolution) error {
	fake.calls = append(fake.calls, "Destroy")
	return nil
}

func (fake *fakeService) Open(_ context.Context, _ state.BoxRecord) error {
	fake.calls = append(fake.calls, "Open")
	return nil
}

func (fake *fakeService) Doctor(_ context.Context, request app.Request) (any, error) {
	fake.calls = append(fake.calls, "Doctor:"+request.Path)
	return fake.doctorData, nil
}

func TestRunEnsuresBoxThenPassesChildArgsAndEnvironment(t *testing.T) {
	fake := newFakeService()
	root := cli.New(cli.Dependencies{Service: fake}, testStreams())
	root.SetArgs([]string{
		"--path", "/repo",
		"--env", "TOKEN=value with spaces",
		"run", "--", "docker", "compose", "up", "-d",
	})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	assertCalls(t, fake.calls,
		"Resolve:/repo",
		"Ensure",
		"Run:docker|compose|up|-d",
		"Env:TOKEN=value with spaces",
		"SyncLoopback",
	)
}

func TestExecStoppedBoxReturnsActionableError(t *testing.T) {
	fake := newFakeService()
	fake.status = state.Stopped
	root := cli.New(cli.Dependencies{Service: fake}, testStreams())
	root.SetArgs([]string{"exec", "--", "pwd"})

	err := root.Execute()

	if err == nil ||
		!strings.Contains(err.Error(), `wktbox up`) ||
		!strings.Contains(err.Error(), `wktbox run --`) {
		t.Fatalf("error = %v", err)
	}
	assertCalls(t, fake.calls, "Resolve:.", "Inspect")
}

func TestComposePrefixesDockerComposeAndEnsuresBox(t *testing.T) {
	fake := newFakeService()
	root := cli.New(cli.Dependencies{Service: fake}, testStreams())
	root.SetArgs([]string{"compose", "--", "up", "--build", "-d"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	assertCalls(t, fake.calls,
		"Resolve:.",
		"Ensure",
		"Run:docker|compose|up|--build|-d",
		"Env:",
		"SyncLoopback",
	)
}

func TestRunSyncsLoopbackAfterChildAndPreservesStdout(t *testing.T) {
	fake := newFakeService()
	fake.childOut = "child-output"
	fake.syncStatus.Routes = []loopback.Route{{
		Port:     5173,
		Target:   "docker:5173",
		Sources:  []string{"frontend"},
		Protocol: "tcp",
		State:    loopback.RouteListening,
	}}
	streams := testStreams()
	root := cli.New(cli.Dependencies{Service: fake}, streams)
	root.SetArgs([]string{"run", "--", "printf", "child-output"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	if got := streams.Out.(*bytes.Buffer).String(); got != "child-output" {
		t.Fatalf("stdout = %q", got)
	}
	if got := streams.Err.(*bytes.Buffer).String(); !strings.Contains(
		got,
		"http://localhost:5173",
	) {
		t.Fatalf("stderr = %q", got)
	}
}

func TestExecDoesNotSyncLoopback(t *testing.T) {
	fake := newFakeService()
	root := cli.New(cli.Dependencies{Service: fake}, testStreams())
	root.SetArgs([]string{"exec", "--", "true"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	assertCalls(t, fake.calls,
		"Resolve:.",
		"Inspect",
		"Run:true",
		"Env:",
	)
}

func TestFailedChildDoesNotSyncOrReplaceItsExitCode(t *testing.T) {
	fake := newFakeService()
	fake.runCode = 23
	root := cli.New(cli.Dependencies{Service: fake}, testStreams())
	root.SetArgs([]string{"compose", "--", "up", "-d"})

	err := root.Execute()

	var exitError cli.ExitError
	if !errors.As(err, &exitError) || exitError.Code != 23 {
		t.Fatalf("error = %#v", err)
	}
	for _, call := range fake.calls {
		if call == "SyncLoopback" {
			t.Fatalf("unexpected calls = %#v", fake.calls)
		}
	}
}

func TestQuietSuppressesLoopbackSummaryOnly(t *testing.T) {
	fake := newFakeService()
	fake.childOut = "child-output"
	fake.syncStatus.Routes = []loopback.Route{{
		Port:  5173,
		State: loopback.RouteListening,
	}}
	streams := testStreams()
	root := cli.New(cli.Dependencies{Service: fake}, streams)
	root.SetArgs([]string{"--quiet", "run", "--", "true"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := streams.Out.(*bytes.Buffer).String(); got != "child-output" {
		t.Fatalf("stdout = %q", got)
	}
	if got := streams.Err.(*bytes.Buffer).String(); got != "" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestUpDoesNotExecuteAChildCommand(t *testing.T) {
	fake := newFakeService()
	root := cli.New(cli.Dependencies{Service: fake}, testStreams())
	root.SetArgs([]string{"up"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	assertCalls(t, fake.calls, "Resolve:.", "Ensure")
}

func TestDestroyRequiresForceWithoutTerminal(t *testing.T) {
	fake := newFakeService()
	streams := testStreams()
	streams.Terminal = false
	root := cli.New(cli.Dependencies{Service: fake}, streams)
	root.SetArgs([]string{"destroy"})

	err := root.Execute()

	if !errors.Is(err, cli.ErrForceRequired) {
		t.Fatalf("error = %v", err)
	}
	assertCalls(t, fake.calls, "Resolve:.", "Inspect")
}

func TestDestroyForceSkipsPrompt(t *testing.T) {
	fake := newFakeService()
	streams := testStreams()
	streams.Terminal = false
	root := cli.New(cli.Dependencies{Service: fake}, streams)
	root.SetArgs([]string{"destroy", "--force"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	assertCalls(t, fake.calls, "Resolve:.", "Inspect", "Destroy")
}

func TestDestroyInteractiveRequiresAffirmativeConfirmation(t *testing.T) {
	fake := newFakeService()
	streams := testStreams()
	streams.In = strings.NewReader("yes\n")
	streams.Terminal = true
	root := cli.New(cli.Dependencies{Service: fake}, streams)
	root.SetArgs([]string{"destroy"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	assertCalls(t, fake.calls, "Resolve:.", "Inspect", "Destroy")
}

func TestLogsPreservesServiceFollowAndOutput(t *testing.T) {
	fake := newFakeService()
	fake.logOutput = "docker | ready\n"
	streams := testStreams()
	root := cli.New(cli.Dependencies{Service: fake}, streams)
	root.SetArgs([]string{"logs", "docker", "--follow"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	assertCalls(t, fake.calls, "Resolve:.", "Inspect", "Logs:docker:true")
	if got := streams.Out.(*bytes.Buffer).String(); got != fake.logOutput {
		t.Fatalf("stdout = %q", got)
	}
}

func TestChildExitCodeIsReturnedWithoutWrappingAsRuntimeFailure(t *testing.T) {
	fake := newFakeService()
	fake.runCode = 42
	root := cli.New(cli.Dependencies{Service: fake}, testStreams())
	root.SetArgs([]string{"run", "--", "false"})

	err := root.Execute()

	var exitError cli.ExitError
	if !errors.As(err, &exitError) || exitError.Code != 42 {
		t.Fatalf("error = %#v", err)
	}
}

func TestRootHelpListsEveryMVPCommand(t *testing.T) {
	fake := newFakeService()
	streams := testStreams()
	root := cli.New(cli.Dependencies{Service: fake}, streams)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	help := streams.Out.(*bytes.Buffer).String()
	for _, name := range []string{
		"up", "run", "exec", "compose", "shell", "open", "list",
		"status", "logs", "stop", "restart", "destroy", "doctor",
		"prune",
	} {
		if !strings.Contains(help, name) {
			t.Errorf("help does not list %q:\n%s", name, help)
		}
	}
}

func TestPruneDefaultsToDryRunAndExplainsForce(t *testing.T) {
	fake := newFakeService()
	fake.pruneData = testPruneReport()
	streams := testStreams()
	root := cli.New(cli.Dependencies{Service: fake}, streams)
	root.SetArgs([]string{"prune"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	assertCalls(t, fake.calls, "Prune:false")
	got := streams.Out.(*bytes.Buffer).String()
	for _, want := range []string{
		"feature-auth",
		"/removed/feature-auth",
		"--force",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prune output missing %q:\n%s", want, got)
		}
	}
}

func TestPruneForceDelegatesDestruction(t *testing.T) {
	fake := newFakeService()
	fake.pruneData = testPruneReport()
	root := cli.New(
		cli.Dependencies{Service: fake},
		testStreams(),
	)
	root.SetArgs([]string{"prune", "--force"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	assertCalls(t, fake.calls, "Prune:true")
}

func TestPruneJSONUsesPublicReport(t *testing.T) {
	fake := newFakeService()
	fake.pruneData = testPruneReport()
	streams := testStreams()
	root := cli.New(cli.Dependencies{Service: fake}, streams)
	root.SetArgs([]string{"--json", "prune"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	var got prune.Report
	if err := json.Unmarshal(
		streams.Out.(*bytes.Buffer).Bytes(),
		&got,
	); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, fake.pruneData) {
		t.Fatalf("report = %#v, want %#v", got, fake.pruneData)
	}
}

func TestAgentsHelpListsInstallStatusAndUninstall(t *testing.T) {
	streams := testStreams()
	root := cli.New(cli.Dependencies{}, streams)
	root.SetArgs([]string{"agents", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	help := streams.Out.(*bytes.Buffer).String()
	for _, command := range []string{"install", "status", "uninstall"} {
		if !strings.Contains(help, command) {
			t.Errorf("agents help missing %q:\n%s", command, help)
		}
	}
}

func TestAgentsInstallDefaultsToAll(t *testing.T) {
	agents := &fakeAgentManager{report: testAgentReport()}
	root := cli.New(cli.Dependencies{Agents: agents}, testStreams())
	root.SetArgs([]string{"agents", "install"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	assertCalls(t, agents.calls, "install:all:dry=false:force=false")
}

func TestAgentsAcceptEachConcreteTarget(t *testing.T) {
	for _, target := range []string{"codex", "claude", "all"} {
		t.Run(target, func(t *testing.T) {
			agents := &fakeAgentManager{report: testAgentReport()}
			root := cli.New(cli.Dependencies{Agents: agents}, testStreams())
			root.SetArgs([]string{
				"agents", "status", "--target", target,
			})

			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}

			assertCalls(
				t,
				agents.calls,
				"status:"+target+":dry=false:force=false",
			)
		})
	}
}

func TestAgentsRejectInvalidTargetWithoutCallingManager(t *testing.T) {
	agents := &fakeAgentManager{report: testAgentReport()}
	root := cli.New(cli.Dependencies{Agents: agents}, testStreams())
	root.SetArgs([]string{"agents", "status", "--target", "copilot"})

	err := root.Execute()

	if err == nil || !strings.Contains(err.Error(), "target") {
		t.Fatalf("error = %v", err)
	}
	if len(agents.calls) != 0 {
		t.Fatalf("calls = %#v", agents.calls)
	}
}

func TestAgentsInstallPassesDryRunAndForce(t *testing.T) {
	agents := &fakeAgentManager{report: testAgentReport()}
	root := cli.New(cli.Dependencies{Agents: agents}, testStreams())
	root.SetArgs([]string{
		"agents", "install",
		"--target", "codex",
		"--dry-run",
		"--force",
	})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	assertCalls(t, agents.calls, "install:codex:dry=true:force=true")
}

func TestAgentsUninstallPassesDryRunAndForce(t *testing.T) {
	agents := &fakeAgentManager{report: testAgentReport()}
	root := cli.New(cli.Dependencies{Agents: agents}, testStreams())
	root.SetArgs([]string{
		"agents", "uninstall",
		"--target", "claude",
		"--dry-run",
		"--force",
	})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	assertCalls(t, agents.calls, "uninstall:claude:dry=true:force=true")
}

func TestAgentsStatusUsesRootJSONContract(t *testing.T) {
	agents := &fakeAgentManager{report: testAgentReport()}
	streams := testStreams()
	root := cli.New(cli.Dependencies{Agents: agents}, streams)
	root.SetArgs([]string{
		"--json", "agents", "status", "--target", "codex",
	})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	var got agentintegration.Report
	if err := json.Unmarshal(
		streams.Out.(*bytes.Buffer).Bytes(),
		&got,
	); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, agents.report) {
		t.Fatalf("report = %#v, want %#v", got, agents.report)
	}
}

func TestAgentsPropagatesConflict(t *testing.T) {
	agents := &fakeAgentManager{err: agentintegration.ErrConflict}
	root := cli.New(cli.Dependencies{Agents: agents}, testStreams())
	root.SetArgs([]string{"agents", "install", "--target", "codex"})

	err := root.Execute()

	if !errors.Is(err, agentintegration.ErrConflict) {
		t.Fatalf("error = %v", err)
	}
}

func TestAgentsStatusNeverCallsMutationMethods(t *testing.T) {
	agents := &fakeAgentManager{report: testAgentReport()}
	root := cli.New(cli.Dependencies{Agents: agents}, testStreams())
	root.SetArgs([]string{"agents", "status"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	assertCalls(t, agents.calls, "status:all:dry=false:force=false")
}

func testStreams() cli.Streams {
	return cli.Streams{
		In:         strings.NewReader(""),
		Out:        &bytes.Buffer{},
		Err:        &bytes.Buffer{},
		Terminal:   true,
		WorkingDir: "/repo",
	}
}

func testAgentReport() agentintegration.Report {
	return agentintegration.Report{Targets: []agentintegration.TargetStatus{{
		Target:           agentintegration.TargetCodex,
		Installed:        true,
		Version:          "0.1.0",
		SkillPath:        "/home/test/.agents/skills/wktbox-isolated-development",
		InstructionsPath: "/home/test/.codex/AGENTS.md",
		ManagedBlock:     true,
		ExpectedDigest:   "sha256:expected",
		ActualDigest:     "sha256:expected",
	}}}
}

func testPruneReport() prune.Report {
	candidate := prune.Candidate{
		ID:       "a4f8c9137d2b",
		Name:     "feature-auth",
		Worktree: "/removed/feature-auth",
	}
	return prune.Report{
		Candidates: []prune.Candidate{candidate},
		Destroyed:  []prune.Candidate{},
		Warnings: []prune.Warning{{
			ID:      "bbbbbbbbbbbb",
			Path:    "/private/worktree",
			Message: "inspect recorded worktree: permission denied",
		}},
	}
}

func agentCall(operation string, options agentintegration.Options) string {
	return operation + ":" + string(options.Target) +
		":dry=" + boolString(options.DryRun) +
		":force=" + boolString(options.Force)
}

func assertCalls(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %#v, want %#v", got, want)
	}
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
