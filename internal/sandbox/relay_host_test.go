package sandbox

import (
	"context"
	"errors"
	"strings"
	"testing"

	"wktbox/internal/ports"
	"wktbox/internal/process"
	"wktbox/internal/state"
)

type relayHostRunner struct {
	operatingSystem string
	gateways        string
	err             error
	calls           int
}

func (runner *relayHostRunner) Run(
	_ context.Context,
	_ string,
	arguments ...string,
) (process.Result, error) {
	runner.calls++
	if runner.err != nil {
		return process.Result{}, runner.err
	}
	if arguments[0] == "info" {
		return process.Result{Stdout: runner.operatingSystem}, nil
	}
	return process.Result{Stdout: runner.gateways}, nil
}

func noEnvironment(string) string { return "" }

func TestDiscoverRelayHostUsesBridgeGatewayOnLinuxEngine(t *testing.T) {
	runner := &relayHostRunner{
		operatingSystem: "Ubuntu 26.04 LTS\n",
		gateways:        "fd00::1 172.17.0.1 \n",
	}
	got := discoverRelayHost(context.Background(), runner, noEnvironment, "linux")
	if got != "172.17.0.1" {
		t.Fatalf("relay host = %q", got)
	}
}

func TestDiscoverRelayHostNeverDefaultsToEveryInterface(t *testing.T) {
	for name, test := range map[string]struct {
		runner *relayHostRunner
		goos   string
	}{
		"docker desktop on linux": {
			runner: &relayHostRunner{operatingSystem: "Docker Desktop\n", gateways: "172.17.0.1"},
			goos:   "linux",
		},
		"macos":          {runner: &relayHostRunner{}, goos: "darwin"},
		"windows":        {runner: &relayHostRunner{}, goos: "windows"},
		"no gateway":     {runner: &relayHostRunner{operatingSystem: "Fedora", gateways: "\n"}, goos: "linux"},
		"docker failure": {runner: &relayHostRunner{err: errors.New("daemon unavailable")}, goos: "linux"},
	} {
		t.Run(name, func(t *testing.T) {
			got := discoverRelayHost(context.Background(), test.runner, noEnvironment, test.goos)
			if got != loopbackRelayHost {
				t.Fatalf("relay host = %q", got)
			}
		})
	}
}

func TestDiscoverRelayHostHonorsExplicitBind(t *testing.T) {
	runner := &relayHostRunner{operatingSystem: "Ubuntu", gateways: "172.17.0.1"}
	for value, want := range map[string]string{
		"0.0.0.0":    "0.0.0.0",
		" 10.0.0.5 ": "10.0.0.5",
		"not-an-ip":  "172.17.0.1",
		"fd00::1":    "172.17.0.1",
	} {
		getenv := func(name string) string {
			if name == relayBindVariable {
				return value
			}
			return ""
		}
		if got := discoverRelayHost(context.Background(), runner, getenv, "linux"); got != want {
			t.Fatalf("%s=%q: relay host = %q, want %q", relayBindVariable, value, got, want)
		}
	}
}

func TestNativeRelayHostCachesDiscovery(t *testing.T) {
	runner := &relayHostRunner{operatingSystem: "Ubuntu", gateways: "172.17.0.1"}
	host := nativeRelayHost(runner)
	first := host(context.Background())
	calls := runner.calls
	if second := host(context.Background()); second != first || runner.calls != calls {
		t.Fatalf("relay host discovery was not cached: %q %q, %d calls", first, second, runner.calls)
	}
}

func TestRelayOptionsProbeTheBoundAddress(t *testing.T) {
	record := state.BoxRecord{
		PortConfigPath: "/state/box/ports/ports.json",
		Ports:          ports.Block{Start: 23000, Size: 10},
	}
	bound := Manager{relayHost: func(context.Context) string { return "172.17.0.1" }}
	options := bound.relayOptions(context.Background(), record)
	if !strings.HasPrefix(options.ListenAddress, "172.17.0.1:") ||
		options.ProbeAddress != options.ListenAddress {
		t.Fatalf("bound relay options = %#v", options)
	}
	wildcard := Manager{relayHost: func(context.Context) string { return "0.0.0.0" }}
	options = wildcard.relayOptions(context.Background(), record)
	if !strings.HasPrefix(options.ListenAddress, "0.0.0.0:") ||
		!strings.HasPrefix(options.ProbeAddress, "127.0.0.1:") {
		t.Fatalf("wildcard relay options = %#v", options)
	}
}
