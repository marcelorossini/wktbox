package sandbox

import (
	"context"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"wktbox/internal/process"
)

// loopbackRelayHost keeps the relay on host loopback. Docker Desktop forwards
// host.docker.internal connections from its VM to host loopback, so the relay
// stays unreachable from external interfaces there as well.
const loopbackRelayHost = "127.0.0.1"

// relayBindVariable overrides the relay address for hosts where neither the
// bridge gateway nor loopback is reachable from containers. Setting 0.0.0.0
// is an explicit opt-in to listening on every interface.
const relayBindVariable = "WKTBOX_RELAY_BIND"

// nativeRelayHost returns the IPv4 address on which the host import relay
// listens. On a native Linux engine, containers reach host-gateway through the
// default bridge gateway, so binding there keeps the relay off external
// interfaces. Reachability from the box is verified separately by RelayProbe.
func nativeRelayHost(runner process.Runner) func(context.Context) string {
	var mutex sync.Mutex
	cached := ""
	return func(ctx context.Context) string {
		mutex.Lock()
		defer mutex.Unlock()
		if cached == "" {
			cached = discoverRelayHost(ctx, runner, os.Getenv, runtime.GOOS)
		}
		return cached
	}
}

func discoverRelayHost(
	ctx context.Context,
	runner process.Runner,
	getenv func(string) string,
	goos string,
) string {
	if override := strings.TrimSpace(getenv(relayBindVariable)); override != "" {
		if address := net.ParseIP(override); address != nil && address.To4() != nil {
			return address.String()
		}
	}
	if goos != "linux" {
		return loopbackRelayHost
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	info, err := runner.Run(
		ctx,
		"docker",
		"info",
		"--format",
		"{{.OperatingSystem}}",
	)
	if err != nil || strings.Contains(info.Stdout, "Docker Desktop") {
		return loopbackRelayHost
	}
	result, err := runner.Run(
		ctx,
		"docker",
		"network",
		"inspect",
		"bridge",
		"--format",
		"{{range .IPAM.Config}}{{.Gateway}} {{end}}",
	)
	if err != nil {
		return loopbackRelayHost
	}
	return bridgeGatewayIPv4(result.Stdout)
}

func bridgeGatewayIPv4(output string) string {
	for _, field := range strings.Fields(output) {
		address := net.ParseIP(field)
		if address != nil && address.To4() != nil && !address.IsUnspecified() {
			return address.String()
		}
	}
	return loopbackRelayHost
}
