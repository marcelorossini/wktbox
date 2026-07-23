package compose_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"wktbox/internal/compose"
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

func TestInspectDerivesReadyFromDockerHealthAndRunnerState(t *testing.T) {
	runner := &recordingRunner{results: []process.Result{{Stdout: `[
		{"Name":"wktbox-a-docker-1","Service":"docker","State":"running","Health":"healthy"},
		{"Name":"wktbox-a-webtop-1","Service":"webtop","State":"running","Health":""}
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

func TestInspectReportsStoppedWhenNoContainerRuns(t *testing.T) {
	runner := &recordingRunner{results: []process.Result{{Stdout: `[
		{"Name":"wktbox-a-docker-1","Service":"docker","State":"exited","Health":""},
		{"Name":"wktbox-a-webtop-1","Service":"webtop","State":"exited","Health":""}
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
		"container-b\twebtop-a\trunning\ta4f8c9137d2b\t/repo a\twktbox-a4f8c9137d2b\twebtop\tUp 1 minute\t127.0.0.1:23000->3000/tcp",
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
