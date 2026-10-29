package sandbox

import (
	"context"
	"errors"
	"runtime"
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

func TestNativeRelayHostUsesBridgeGatewayOnLinuxEngine(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("bridge binding applies only to native Linux engines")
	}
	runner := &relayHostRunner{
		operatingSystem: "Ubuntu 26.04 LTS\n",
		gateways:        "fd00::1 172.17.0.1 \n",
	}
	host := nativeRelayHost(runner)
	if got := host(context.Background()); got != "172.17.0.1" {
		t.Fatalf("relay host = %q", got)
	}
	host(context.Background())
	if runner.calls != 2 {
		t.Fatalf("relay host discovery was not cached: %d calls", runner.calls)
	}
}

func TestNativeRelayHostFallsBackToWildcard(t *testing.T) {
	for name, runner := range map[string]*relayHostRunner{
		"docker desktop": {operatingSystem: "Docker Desktop\n", gateways: "172.17.0.1"},
		"no gateway":     {operatingSystem: "Fedora", gateways: "\n"},
		"docker failure": {err: errors.New("daemon unavailable")},
	} {
		t.Run(name, func(t *testing.T) {
			if got := nativeRelayHost(runner)(context.Background()); got != wildcardRelayHost {
				t.Fatalf("relay host = %q", got)
			}
		})
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
	wildcard := Manager{relayHost: func(context.Context) string { return wildcardRelayHost }}
	options = wildcard.relayOptions(context.Background(), record)
	if !strings.HasPrefix(options.ListenAddress, "0.0.0.0:") ||
		!strings.HasPrefix(options.ProbeAddress, "127.0.0.1:") {
		t.Fatalf("wildcard relay options = %#v", options)
	}
}
