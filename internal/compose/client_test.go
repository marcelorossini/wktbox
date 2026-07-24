package compose_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"wktbox/internal/compose"
	"wktbox/internal/portforward"
	"wktbox/internal/ports"
	"wktbox/internal/process"
)

type recordingRunner struct {
	calls   [][]string
	results []process.Result
	errors  []error
}

func (runner *recordingRunner) Run(
	_ context.Context,
	name string,
	arguments ...string,
) (process.Result, error) {
	call := append([]string{name}, arguments...)
	runner.calls = append(runner.calls, call)
	index := len(runner.calls) - 1
	var result process.Result
	if index < len(runner.results) {
		result = runner.results[index]
	}
	var err error
	if index < len(runner.errors) {
		err = runner.errors[index]
	}
	return result, err
}

func TestDownRemovesVolumesOnlyForSelectedProject(t *testing.T) {
	runner := &recordingRunner{}
	client := compose.NewClient(runner)
	err := client.Down(context.Background(), compose.Project{
		Name:    "wktbox-a4f8c9137d2b",
		Files:   []string{"/state/a/compose.yml", "/state/a/env.override.yml"},
		EnvFile: "/state/a/sandbox.env",
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"docker", "compose",
		"-p", "wktbox-a4f8c9137d2b",
		"--env-file", "/state/a/sandbox.env",
		"-f", "/state/a/compose.yml",
		"-f", "/state/a/env.override.yml",
		"down", "--volumes", "--remove-orphans",
	}
	if !reflect.DeepEqual(runner.calls[0], want) {
		t.Fatalf("call = %#v\nwant = %#v", runner.calls[0], want)
	}
}

func TestUpEnablesOnlyConfiguredGatewayProfile(t *testing.T) {
	runner := &recordingRunner{}
	client := compose.NewClient(runner)
	err := client.Up(context.Background(), compose.Project{
		Name:           "wktbox-a4f8c9137d2b",
		Files:          []string{"/state/a/compose.yml"},
		EnvFile:        "/state/a/sandbox.env",
		GatewayEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.calls[0], " ")
	if !strings.Contains(got, "--profile gateway") {
		t.Fatalf("gateway profile missing: %s", got)
	}
	if !strings.HasSuffix(got, "up -d --wait") {
		t.Fatalf("up flags = %s", got)
	}
}

func TestUpIncludesPortOverrideAndEnablesPortsProfile(t *testing.T) {
	runner := &recordingRunner{}
	client := compose.NewClient(runner)
	err := client.Up(context.Background(), compose.Project{
		Name:                "wktbox-a4f8c9137d2b",
		Files:               []string{"/state/a/compose.yml", "/state/a/ports.override.yml"},
		EnvFile:             "/state/a/sandbox.env",
		PortMappingsEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"docker", "compose",
		"-p", "wktbox-a4f8c9137d2b",
		"--env-file", "/state/a/sandbox.env",
		"-f", "/state/a/compose.yml",
		"-f", "/state/a/ports.override.yml",
		"--profile", "ports",
		"up", "-d", "--wait",
	}
	if !reflect.DeepEqual(runner.calls[0], want) {
		t.Fatalf("call = %#v\nwant = %#v", runner.calls[0], want)
	}
}

func TestStartUsesPortableUpWait(t *testing.T) {
	runner := &recordingRunner{}
	client := compose.NewClient(runner)
	err := client.Start(context.Background(), testProject())
	if err != nil {
		t.Fatal(err)
	}

	got := strings.Join(runner.calls[0], " ")
	if !strings.HasSuffix(got, "up -d --wait") {
		t.Fatalf("start command = %s", got)
	}
	if strings.Contains(got, " start ") {
		t.Fatalf("start uses unsupported compose start --wait: %s", got)
	}
}

func TestRestartStopsThenStartsServicesInDependencyOrderAndWaits(t *testing.T) {
	runner := &recordingRunner{}
	if err := compose.NewClient(runner).Restart(
		context.Background(),
		testProject(),
	); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %#v", runner.calls)
	}
	if got := strings.Join(runner.calls[0], " "); !strings.HasSuffix(got, "stop") {
		t.Fatalf("stop call = %s", got)
	}
	if got := strings.Join(runner.calls[1], " "); !strings.HasSuffix(
		got,
		"up -d --wait",
	) {
		t.Fatalf("readiness call = %s", got)
	}
	for _, call := range runner.calls {
		if strings.Contains(strings.Join(call, " "), " restart") {
			t.Fatalf("restart bypassed dependency ordering: %#v", runner.calls)
		}
	}
}

func TestInspectDerivesReadyFromDockerHealthAndRunnerState(t *testing.T) {
	runner := &recordingRunner{results: []process.Result{{Stdout: `[
		{"Name":"wktbox-a-docker-1","Service":"docker","State":"running","Health":"healthy"},
		{"Name":"wktbox-a-webtop-1","Service":"webtop","State":"running","Health":"healthy"},
		{"Name":"wktbox-a-loopback-1","Service":"loopback","State":"running","Health":"healthy"},
		{"Name":"wktbox-a-interconnect-1","Service":"interconnect","State":"running","Health":"healthy"}
	]`}}}
	client := compose.NewClient(runner)

	got, err := client.Inspect(context.Background(), testProject())
	if err != nil {
		t.Fatal(err)
	}
	if !got.Exists || !got.Ready() || got.State != compose.Running {
		t.Fatalf("status = %#v", got)
	}
}

func TestInspectRequiresHealthyWebtopBrowser(t *testing.T) {
	runner := &recordingRunner{results: []process.Result{{Stdout: `[
		{"Name":"wktbox-a-docker-1","Service":"docker","State":"running","Health":"healthy"},
		{"Name":"wktbox-a-webtop-1","Service":"webtop","State":"running","Health":"starting"},
		{"Name":"wktbox-a-loopback-1","Service":"loopback","State":"running","Health":"healthy"},
		{"Name":"wktbox-a-interconnect-1","Service":"interconnect","State":"running","Health":"healthy"}
	]`}}}

	got, err := compose.NewClient(runner).Inspect(context.Background(), testProject())
	if err != nil {
		t.Fatal(err)
	}
	if got.Ready() {
		t.Fatalf("status unexpectedly ready: %#v", got)
	}
}

func TestInspectRequiresHealthyLoopback(t *testing.T) {
	runner := &recordingRunner{results: []process.Result{{Stdout: `[
		{"Name":"wktbox-a-docker-1","Service":"docker","State":"running","Health":"healthy"},
		{"Name":"wktbox-a-webtop-1","Service":"webtop","State":"running","Health":"healthy"},
		{"Name":"wktbox-a-loopback-1","Service":"loopback","State":"running","Health":"starting"},
		{"Name":"wktbox-a-interconnect-1","Service":"interconnect","State":"running","Health":"healthy"}
	]`}}}

	got, err := compose.NewClient(runner).Inspect(context.Background(), testProject())
	if err != nil {
		t.Fatal(err)
	}
	if got.Ready() {
		t.Fatalf("status unexpectedly ready: %#v", got)
	}
}

func TestInspectRequiresHealthyInterconnect(t *testing.T) {
	runner := &recordingRunner{results: []process.Result{{Stdout: `[
		{"Name":"wktbox-a-docker-1","Service":"docker","State":"running","Health":"healthy"},
		{"Name":"wktbox-a-webtop-1","Service":"webtop","State":"running","Health":"healthy"},
		{"Name":"wktbox-a-loopback-1","Service":"loopback","State":"running","Health":"healthy"},
		{"Name":"wktbox-a-interconnect-1","Service":"interconnect","State":"running","Health":"starting"}
	]`}}}

	got, err := compose.NewClient(runner).Inspect(context.Background(), testProject())
	if err != nil {
		t.Fatal(err)
	}
	if got.Ready() {
		t.Fatalf("status unexpectedly ready: %#v", got)
	}
}

func TestStatusReadyForPublicationsRequiresRunningPortBridge(t *testing.T) {
	status := compose.Status{
		Exists: true,
		State:  compose.Running,
		Containers: []compose.ContainerStatus{
			{Service: "docker", State: "running", Health: "healthy"},
			{Service: "webtop", State: "running", Health: "healthy"},
			{Service: "loopback", State: "running", Health: "healthy"},
			{Service: "interconnect", State: "running", Health: "healthy"},
		},
	}
	project := testProject()
	project.PortMappingsEnabled = true

	if status.ReadyFor(project) {
		t.Fatal("publication project is ready without portbridge")
	}
	status.Containers = append(status.Containers, compose.ContainerStatus{
		Service: "portbridge",
		State:   "running",
	})
	if !status.ReadyFor(project) {
		t.Fatalf("publication project is not ready with portbridge: %#v", status)
	}
}

func TestInspectReportsStoppedWhenNoContainerRuns(t *testing.T) {
	runner := &recordingRunner{results: []process.Result{{Stdout: `[
		{"Name":"wktbox-a-docker-1","Service":"docker","State":"exited","Health":""},
		{"Name":"wktbox-a-webtop-1","Service":"webtop","State":"exited","Health":""},
		{"Name":"wktbox-a-loopback-1","Service":"loopback","State":"exited","Health":""},
		{"Name":"wktbox-a-interconnect-1","Service":"interconnect","State":"exited","Health":""}
	]`}}}
	client := compose.NewClient(runner)

	got, err := client.Inspect(context.Background(), testProject())
	if err != nil {
		t.Fatal(err)
	}
	if got.State != compose.Stopped || got.Ready() {
		t.Fatalf("status = %#v", got)
	}
}

func TestListManagedGroupsContainersByBoxLabel(t *testing.T) {
	output := strings.Join([]string{
		"container-a\tdocker-a\trunning\ta4f8c9137d2b\t/repo a\twktbox-a4f8c9137d2b\tdocker\tUp 1 minute (healthy)\t",
		"container-b\twebtop-a\trunning\ta4f8c9137d2b\t/repo a\twktbox-a4f8c9137d2b\twebtop\tUp 1 minute (healthy)\t127.0.0.1:23000->61000/tcp",
		"container-d\tloopback-a\trunning\ta4f8c9137d2b\t/repo a\twktbox-a4f8c9137d2b\tloopback\tUp 1 minute (healthy)\t",
		"container-e\tinterconnect-a\trunning\ta4f8c9137d2b\t/repo a\twktbox-a4f8c9137d2b\tinterconnect\tUp 1 minute (healthy)\t",
		"container-c\tgateway-a\trunning\ta4f8c9137d2b\t/repo a\twktbox-a4f8c9137d2b\tgateway\tUp 1 minute\t127.0.0.1:23003->8080/tcp",
	}, "\n")
	runner := &recordingRunner{results: []process.Result{{Stdout: output}}}
	client := compose.NewClient(runner)

	got, err := client.ListManaged(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("managed projects = %#v", got)
	}
	if got[0].ID != "a4f8c9137d2b" || got[0].Worktree != "/repo a" ||
		got[0].ProjectName != "wktbox-a4f8c9137d2b" || got[0].State != compose.Running {
		t.Fatalf("managed project = %#v", got[0])
	}
	if !got[0].Healthy || got[0].Ports != (ports.Block{Start: 23000, Size: 10}) {
		t.Fatalf("managed health/ports = %#v", got[0])
	}
	if !got[0].GatewayEnabled {
		t.Fatalf("managed gateway was not recovered: %#v", got[0])
	}
}

func TestListManagedRequiresHealthyWebtopBrowser(t *testing.T) {
	output := strings.Join([]string{
		"container-a\tdocker-a\trunning\ta4f8c9137d2b\t/repo\twktbox-a4f8c9137d2b\tdocker\tUp 1 minute (healthy)\t",
		"container-b\twebtop-a\trunning\ta4f8c9137d2b\t/repo\twktbox-a4f8c9137d2b\twebtop\tUp 1 minute (unhealthy)\t127.0.0.1:23000->61000/tcp",
		"container-d\tloopback-a\trunning\ta4f8c9137d2b\t/repo\twktbox-a4f8c9137d2b\tloopback\tUp 1 minute (healthy)\t",
		"container-e\tinterconnect-a\trunning\ta4f8c9137d2b\t/repo\twktbox-a4f8c9137d2b\tinterconnect\tUp 1 minute (healthy)\t",
	}, "\n")
	runner := &recordingRunner{results: []process.Result{{Stdout: output}}}

	got, err := compose.NewClient(runner).ListManaged(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Healthy {
		t.Fatalf("managed projects = %#v", got)
	}
}

func TestLoopbackSyncExecutesPrivateSidecarCommand(t *testing.T) {
	runner := &recordingRunner{results: []process.Result{{Stdout: `{"eventStream":"connected","routes":[{"port":5173,"target":"docker:5173","protocol":"tcp","state":"listening","sources":["frontend"]}],"warnings":[]}`}}}

	got, err := compose.NewClient(runner).LoopbackSync(context.Background(), testProject())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Routes) != 1 || got.Routes[0].Port != 5173 {
		t.Fatalf("status = %#v", got)
	}
	wantSuffix := "exec -T loopback wktbox-loopback sync --json"
	if call := strings.Join(runner.calls[0], " "); !strings.HasSuffix(call, wantSuffix) {
		t.Fatalf("call = %s", call)
	}
}

func TestLoopbackStatusExecutesReadOnlySidecarCommand(t *testing.T) {
	runner := &recordingRunner{results: []process.Result{{Stdout: `{"eventStream":"connected","routes":[],"warnings":[]}`}}}

	_, err := compose.NewClient(runner).LoopbackStatus(context.Background(), testProject())
	if err != nil {
		t.Fatal(err)
	}
	wantSuffix := "exec -T loopback wktbox-loopback status --json"
	if call := strings.Join(runner.calls[0], " "); !strings.HasSuffix(call, wantSuffix) {
		t.Fatalf("call = %s", call)
	}
}

func TestPortImportPreflightSendsCandidateBatchToSidecar(t *testing.T) {
	runner := &recordingRunner{}
	mappings := []portforward.Mapping{{
		Name:          "api",
		Direction:     portforward.Import,
		SourceAddress: "127.0.0.1",
		SourcePort:    1234,
		TargetPort:    1234,
	}}
	if err := compose.NewClient(runner).PortImportPreflight(
		context.Background(),
		testProject(),
		mappings,
	); err != nil {
		t.Fatal(err)
	}
	call := strings.Join(runner.calls[0], " ")
	if !strings.Contains(
		call,
		"exec -T loopback wktbox-loopback imports-preflight --mappings ",
	) {
		t.Fatalf("call = %s", call)
	}
}

func TestPortRuntimeApplyChangesOnlyPortBridge(t *testing.T) {
	for _, test := range []struct {
		name    string
		enabled bool
		suffix  string
	}{
		{
			name:    "activate",
			enabled: true,
			suffix:  "up -d --wait portbridge",
		},
		{
			name:    "remove",
			enabled: false,
			suffix:  "rm --stop --force portbridge",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &recordingRunner{}
			project := testProject()
			project.PortMappingsEnabled = test.enabled
			if err := compose.NewClient(runner).PortRuntimeApply(
				context.Background(),
				project,
			); err != nil {
				t.Fatal(err)
			}
			call := strings.Join(runner.calls[0], " ")
			if !strings.HasSuffix(call, test.suffix) {
				t.Fatalf("call = %s", call)
			}
		})
	}
}

func TestEnsureConnectionNetworkCreatesLabeledPrivateBridge(t *testing.T) {
	runner := &recordingRunner{
		results: []process.Result{
			{Stderr: "Error: No such network: wktbox-connect-64f420a77f31"},
			{Stdout: "network-id\n"},
		},
		errors: []error{errors.New("exit status 1"), nil},
	}
	client := compose.NewClient(runner)
	network := compose.ConnectionNetwork{
		ID:         "64f420a77f31",
		Name:       "dev-stack",
		DockerName: "wktbox-connect-64f420a77f31",
		Version:    "dev",
	}

	if err := client.EnsureConnectionNetwork(context.Background(), network); err != nil {
		t.Fatal(err)
	}

	got := strings.Join(runner.calls[1], " ")
	for _, want := range []string{
		"docker network create",
		"--driver bridge",
		"--label io.wktbox.managed=true",
		"--label io.wktbox.connection-id=64f420a77f31",
		"--label io.wktbox.connection-name=dev-stack",
		"--label io.wktbox.version=dev",
		"wktbox-connect-64f420a77f31",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("call %q missing %q", got, want)
		}
	}
}

func TestEnsureConnectionNetworkRejectsUnmanagedNameCollision(t *testing.T) {
	runner := &recordingRunner{results: []process.Result{{Stdout: `{
		"Name":"wktbox-connect-64f420a77f31",
		"Labels":{"owner":"other"},
		"Containers":{}
	}`}}}
	client := compose.NewClient(runner)

	err := client.EnsureConnectionNetwork(context.Background(), compose.ConnectionNetwork{
		ID:         "64f420a77f31",
		DockerName: "wktbox-connect-64f420a77f31",
	})

	if err == nil || !strings.Contains(err.Error(), "not managed by Wktbox") {
		t.Fatalf("error = %v", err)
	}
}

func TestConnectEndpointUsesCurrentServiceContainerAndAlias(t *testing.T) {
	runner := &recordingRunner{results: []process.Result{
		{Stdout: "container-id\n"},
		{},
	}}
	client := compose.NewClient(runner)

	err := client.ConnectConnectionEndpoint(
		context.Background(),
		compose.ConnectionEndpoint{
			Network: "wktbox-connect-64f420a77f31",
			BoxID:   "a4f8c9137d2b",
			Service: "docker",
			Alias:   "a4f8c9137d2b.wktbox",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	wantResolve := "docker ps --filter label=io.wktbox.box-id=a4f8c9137d2b --filter label=com.docker.compose.service=docker --filter status=running --format {{.ID}}"
	if got := strings.Join(runner.calls[0], " "); got != wantResolve {
		t.Fatalf("resolve call = %q", got)
	}
	wantConnect := "docker network connect --alias a4f8c9137d2b.wktbox wktbox-connect-64f420a77f31 container-id"
	if got := strings.Join(runner.calls[1], " "); got != wantConnect {
		t.Fatalf("connect call = %q", got)
	}
}

func TestInspectConnectionNetworkReportsManagedEndpoints(t *testing.T) {
	runner := &recordingRunner{results: []process.Result{
		{Stdout: `{
			"Name":"wktbox-connect-64f420a77f31",
			"Labels":{
				"io.wktbox.managed":"true",
				"io.wktbox.connection-id":"64f420a77f31"
			},
			"Containers":{
				"docker-id":{"Name":"wktbox-a-docker-1","IPv4Address":"172.30.0.2/16"},
				"webtop-id":{"Name":"wktbox-a-webtop-1","IPv4Address":"172.30.0.3/16"}
			}
		}`},
		{Stdout: `{"io.wktbox.box-id":"a4f8c9137d2b","com.docker.compose.service":"docker"}`},
		{Stdout: `{"io.wktbox.box-id":"a4f8c9137d2b","com.docker.compose.service":"webtop"}`},
	}}
	client := compose.NewClient(runner)

	got, err := client.InspectConnectionNetwork(
		context.Background(),
		"wktbox-connect-64f420a77f31",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Exists ||
		got.ConnectionID != "64f420a77f31" ||
		got.Endpoints["a4f8c9137d2b/docker"] != "docker-id" ||
		got.Endpoints["a4f8c9137d2b/webtop"] != "webtop-id" {
		t.Fatalf("status = %#v", got)
	}
}

func TestDisconnectAndRemoveConnectionResourcesAreIdempotent(t *testing.T) {
	runner := &recordingRunner{
		results: []process.Result{
			{Stdout: "container-id\n"},
			{Stderr: "container-id is not connected to network"},
			{Stderr: "Error: No such network"},
		},
		errors: []error{
			nil,
			errors.New("exit status 1"),
			errors.New("exit status 1"),
		},
	}
	client := compose.NewClient(runner)
	endpoint := compose.ConnectionEndpoint{
		Network: "wktbox-connect-64f420a77f31",
		BoxID:   "a4f8c9137d2b",
		Service: "webtop",
	}

	if err := client.DisconnectConnectionEndpoint(context.Background(), endpoint); err != nil {
		t.Fatal(err)
	}
	if err := client.RemoveConnectionNetwork(
		context.Background(),
		endpoint.Network,
	); err != nil {
		t.Fatal(err)
	}
}

func TestLogsForwardsServiceAndFollowFlag(t *testing.T) {
	runner := &recordingRunner{results: []process.Result{{Stdout: "docker log\n"}}}
	client := compose.NewClient(runner)

	result, err := client.Logs(context.Background(), testProject(), "docker", true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "docker log\n" {
		t.Fatalf("stdout = %q", result.Stdout)
	}
	got := strings.Join(runner.calls[0], " ")
	if !strings.HasSuffix(got, "logs --follow docker") {
		t.Fatalf("logs call = %s", got)
	}
}

func TestClientIncludesDockerStderrInError(t *testing.T) {
	runner := &recordingRunner{
		results: []process.Result{{Stderr: "daemon unavailable"}},
		errors:  []error{errors.New("exit status 1")},
	}
	err := compose.NewClient(runner).Stop(context.Background(), testProject())
	if err == nil || !strings.Contains(err.Error(), "daemon unavailable") {
		t.Fatalf("error = %v", err)
	}
}

func testProject() compose.Project {
	return compose.Project{
		Name:    "wktbox-a4f8c9137d2b",
		Files:   []string{"/state/a/compose.yml"},
		EnvFile: "/state/a/sandbox.env",
	}
}
