package cli_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"wktbox/internal/app"
	"wktbox/internal/cli"
	"wktbox/internal/executor"
	"wktbox/internal/loopback"
	"wktbox/internal/ports"
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
	root := cli.New(fake, testStreams())
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
	root := cli.New(fake, testStreams())
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
	root := cli.New(fake, testStreams())
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
	root := cli.New(fake, streams)
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
	root := cli.New(fake, testStreams())
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
	root := cli.New(fake, testStreams())
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
	root := cli.New(fake, streams)
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
	root := cli.New(fake, testStreams())
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
	root := cli.New(fake, streams)
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
	root := cli.New(fake, streams)
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
	root := cli.New(fake, streams)
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
	root := cli.New(fake, streams)
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
	root := cli.New(fake, testStreams())
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
	root := cli.New(fake, streams)
	root.SetArgs([]string{"--help"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	help := streams.Out.(*bytes.Buffer).String()
	for _, name := range []string{
		"up", "run", "exec", "compose", "shell", "open", "list",
		"status", "logs", "stop", "restart", "destroy", "doctor",
	} {
		if !strings.Contains(help, name) {
			t.Errorf("help does not list %q:\n%s", name, help)
		}
	}
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
