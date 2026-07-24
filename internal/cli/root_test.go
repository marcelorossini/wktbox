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
	"time"

	"wktbox/internal/agentintegration"
	"wktbox/internal/app"
	"wktbox/internal/cli"
	"wktbox/internal/executor"
	"wktbox/internal/loopback"
	"wktbox/internal/portforward"
	"wktbox/internal/ports"
	"wktbox/internal/prune"
	"wktbox/internal/state"
)

type fakeService struct {
	calls        []string
	status       state.Status
	runCode      int
	runErr       error
	box          state.BoxRecord
	list         []state.BoxRecord
	logOutput    string
	doctorData   any
	syncStatus   loopback.Status
	syncErr      error
	childOut     string
	pruneData    prune.Report
	pruneErr     error
	connection   state.ConnectionRecord
	connections  []state.ConnectionRecord
	portMappings []portforward.ObservedMapping
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
		connection: state.ConnectionRecord{
			ID:        "64f420a77f31",
			Name:      "dev-stack",
			Network:   "wktbox-connect-64f420a77f31",
			Members:   []string{box.ID},
			Status:    state.ConnectionReady,
			CreatedAt: time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC),
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

func (fake *fakeService) Connect(
	_ context.Context,
	selectors []string,
	name string,
) (state.ConnectionRecord, error) {
	fake.calls = append(
		fake.calls,
		"Connect:"+strings.Join(selectors, "|")+":"+name,
	)
	return fake.connection, nil
}

func (fake *fakeService) Connections(
	_ context.Context,
	selector string,
) ([]state.ConnectionRecord, error) {
	fake.calls = append(fake.calls, "Connections:"+selector)
	if fake.connections != nil {
		return fake.connections, nil
	}
	return []state.ConnectionRecord{fake.connection}, nil
}

func (fake *fakeService) Disconnect(
	_ context.Context,
	selector string,
) (state.ConnectionRecord, error) {
	fake.calls = append(fake.calls, "Disconnect:"+selector)
	return fake.connection, nil
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

func (fake *fakeService) ImportPorts(
	_ context.Context,
	_ state.BoxRecord,
	mappings []portforward.Mapping,
) ([]portforward.ObservedMapping, error) {
	fake.calls = append(fake.calls, "ImportPorts:"+portMappingNames(mappings))
	return fakePortObservations(mappings), nil
}

func (fake *fakeService) PublishPorts(
	_ context.Context,
	_ state.BoxRecord,
	mappings []portforward.Mapping,
) ([]portforward.ObservedMapping, error) {
	fake.calls = append(fake.calls, "PublishPorts:"+portMappingNames(mappings))
	return fakePortObservations(mappings), nil
}

func (fake *fakeService) PortMappings(
	context.Context,
	state.BoxRecord,
) ([]portforward.ObservedMapping, error) {
	fake.calls = append(fake.calls, "PortMappings")
	return fake.portMappings, nil
}

func (fake *fakeService) RemovePortMappings(
	_ context.Context,
	_ state.BoxRecord,
	names []string,
	direction portforward.Direction,
) ([]portforward.ObservedMapping, error) {
	fake.calls = append(
		fake.calls,
		"RemovePortMappings:"+strings.Join(names, "|")+":"+string(direction),
	)
	return fake.portMappings, nil
}

func fakePortObservations(
	mappings []portforward.Mapping,
) []portforward.ObservedMapping {
	result := make([]portforward.ObservedMapping, 0, len(mappings))
	for _, mapping := range mappings {
		result = append(result, portforward.ObservedMapping{
			Mapping: mapping,
			State:   portforward.StateReady,
		})
	}
	return result
}

func portMappingNames(mappings []portforward.Mapping) string {
	names := make([]string, 0, len(mappings))
	for _, mapping := range mappings {
		names = append(names, mapping.Name)
	}
	return strings.Join(names, "|")
}

func TestPortImportAcceptsMultipleHostPortsAsOneBatch(t *testing.T) {
	fake := newFakeService()
	root := cli.New(cli.Dependencies{Service: fake}, testStreams())
	root.SetArgs([]string{"port", "import", "1234", "5432"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	assertCalls(
		t,
		fake.calls,
		"Resolve:.",
		"Inspect",
		"ImportPorts:import-1234|import-5432",
	)
}

func TestPortPublishAcceptsMultipleNamedMapsAsOneBatch(t *testing.T) {
	fake := newFakeService()
	root := cli.New(cli.Dependencies{Service: fake}, testStreams())
	root.SetArgs([]string{
		"port", "publish",
		"--map", "frontend=127.0.0.1:15173:5173",
		"--map", "api=127.0.0.1:18000:8000",
	})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	assertCalls(
		t,
		fake.calls,
		"Resolve:.",
		"Inspect",
		"PublishPorts:api|frontend",
	)
}

func TestPortListJSONNeverContainsRelaySecrets(t *testing.T) {
	fake := newFakeService()
	fake.portMappings = []portforward.ObservedMapping{{
		Mapping: portforward.Mapping{
			Name:          "api",
			Direction:     portforward.Publish,
			SourceAddress: "127.0.0.1",
			SourcePort:    18000,
			TargetPort:    8000,
		},
		State: portforward.StateReady,
	}}
	streams := testStreams()
	root := cli.New(cli.Dependencies{Service: fake}, streams)
	root.SetArgs([]string{"--json", "port", "list"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	output := strings.ToLower(streams.Out.(*bytes.Buffer).String())
	for _, forbidden := range []string{"token", "pid", "relay"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("secret field %q in %s", forbidden, output)
		}
	}
	assertCalls(t, fake.calls, "Resolve:.", "Inspect", "PortMappings")
}

func TestPortListSupportsStoppedBox(t *testing.T) {
	fake := newFakeService()
	fake.status = state.Stopped
	root := cli.New(cli.Dependencies{Service: fake}, testStreams())
	root.SetArgs([]string{"port", "list"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, fake.calls, "Resolve:.", "Inspect", "PortMappings")
}

func TestPortRemoveSupportsAllImportsWithoutNames(t *testing.T) {
	fake := newFakeService()
	root := cli.New(cli.Dependencies{Service: fake}, testStreams())
	root.SetArgs([]string{"port", "remove", "--all-imports"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	assertCalls(
		t,
		fake.calls,
		"Resolve:.",
		"Inspect",
		"RemovePortMappings::import",
	)
}

func TestPortRemoveSupportsStoppedBox(t *testing.T) {
	fake := newFakeService()
	fake.status = state.Stopped
	root := cli.New(cli.Dependencies{Service: fake}, testStreams())
	root.SetArgs([]string{"port", "remove", "api"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	assertCalls(
		t,
		fake.calls,
		"Resolve:.",
		"Inspect",
		"RemovePortMappings:api:",
	)
}

func TestPortCommandsRejectStoppedBoxBeforeMutation(t *testing.T) {
	fake := newFakeService()
	fake.status = state.Stopped
	root := cli.New(cli.Dependencies{Service: fake}, testStreams())
	root.SetArgs([]string{"port", "import", "1234"})

	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "wktbox up") {
		t.Fatalf("error = %v", err)
	}
	assertCalls(t, fake.calls, "Resolve:.", "Inspect")
}

func TestPortImportRejectsMixedSyntaxBeforeResolvingBox(t *testing.T) {
	fake := newFakeService()
	root := cli.New(cli.Dependencies{Service: fake}, testStreams())
	root.SetArgs([]string{
		"port", "import", "1234",
		"--map", "api=127.0.0.1:1234:4321",
	})

	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "either positional ports or --map") {
		t.Fatalf("error = %v", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestPortRemoveRejectsConflictingSelectorsBeforeResolvingBox(t *testing.T) {
	fake := newFakeService()
	root := cli.New(cli.Dependencies{Service: fake}, testStreams())
	root.SetArgs([]string{
		"port", "remove",
		"--all-imports",
		"--all-publications",
	})

	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "provide mapping names") {
		t.Fatalf("error = %v", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestPortHelpListsDirectionalCommands(t *testing.T) {
	streams := testStreams()
	root := cli.New(cli.Dependencies{}, streams)
	root.SetArgs([]string{"port", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	help := streams.Out.(*bytes.Buffer).String()
	for _, command := range []string{"import", "publish", "list", "remove"} {
		if !strings.Contains(help, command) {
			t.Fatalf("port help missing %q:\n%s", command, help)
		}
	}
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
		"prune", "connect", "connections", "disconnect",
	} {
		if !strings.Contains(help, name) {
			t.Errorf("help does not list %q:\n%s", name, help)
		}
	}
}

func TestConnectAcceptsTwoOrMoreSelectorsAndName(t *testing.T) {
	fake := newFakeService()
	fake.connection.Members = []string{
		"a4f8c9137d2b",
		"dfe31c662a91",
		"0a11ce55aa01",
	}
	fake.list = append(fake.list,
		state.BoxRecord{
			ID: "dfe31c662a91", Name: "api", Status: state.Ready,
		},
		state.BoxRecord{
			ID: "0a11ce55aa01", Name: "worker", Status: state.Ready,
		},
	)
	streams := testStreams()
	root := cli.New(cli.Dependencies{Service: fake}, streams)
	root.SetArgs([]string{
		"connect", "a4f", "dfe", "0a1", "--name", "dev-stack",
	})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	assertCalls(
		t,
		fake.calls,
		"Connect:a4f|dfe|0a1:dev-stack",
		"List",
	)
	output := streams.Out.(*bytes.Buffer).String()
	for _, want := range []string{
		"Connection dev-stack",
		"a4f8c9137d2b.wktbox",
		"dfe31c662a91.wktbox",
		"0a11ce55aa01.wktbox",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestConnectRequiresAtLeastTwoSelectors(t *testing.T) {
	fake := newFakeService()
	root := cli.New(cli.Dependencies{Service: fake}, testStreams())
	root.SetArgs([]string{"connect", "a4f"})

	if err := root.Execute(); err == nil {
		t.Fatal("connect unexpectedly accepted one selector")
	}
	if len(fake.calls) != 0 {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestConnectionsListsOrInspectsTopology(t *testing.T) {
	for _, selector := range []string{"", "dev-stack"} {
		t.Run(selector, func(t *testing.T) {
			fake := newFakeService()
			streams := testStreams()
			root := cli.New(cli.Dependencies{Service: fake}, streams)
			arguments := []string{"connections"}
			if selector != "" {
				arguments = append(arguments, selector)
			}
			root.SetArgs(arguments)

			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}

			assertCalls(
				t,
				fake.calls,
				"Connections:"+selector,
				"List",
			)
			if got := streams.Out.(*bytes.Buffer).String(); !strings.Contains(
				got,
				"a4f8c9137d2b.wktbox",
			) {
				t.Fatalf("output = %q", got)
			}
		})
	}
}

func TestDisconnectUsesConnectionSelector(t *testing.T) {
	fake := newFakeService()
	streams := testStreams()
	root := cli.New(cli.Dependencies{Service: fake}, streams)
	root.SetArgs([]string{"disconnect", "dev-stack"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	assertCalls(t, fake.calls, "Disconnect:dev-stack")
	if got := streams.Out.(*bytes.Buffer).String(); !strings.Contains(
		got,
		"dev-stack",
	) {
		t.Fatalf("output = %q", got)
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
