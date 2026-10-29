package sandbox

import (
	"context"
	"net"
	"runtime"
	"strings"
	"sync"
	"time"

	"wktbox/internal/process"
)

// wildcardRelayHost keeps the relay reachable when the bridge gateway cannot
// be determined, such as on Docker Desktop where host.docker.internal is
// forwarded by the VM instead of routed to a local bridge address.
const wildcardRelayHost = "0.0.0.0"

// nativeRelayHost returns the IPv4 address on which the host import relay
// listens. On a native Linux engine, containers reach host-gateway through the
// default bridge gateway, so binding there keeps the relay off external
// interfaces.
func nativeRelayHost(runner process.Runner) func(context.Context) string {
	var mutex sync.Mutex
	cached := ""
	return func(ctx context.Context) string {
		mutex.Lock()
		defer mutex.Unlock()
		if cached == "" {
			cached = discoverRelayHost(ctx, runner)
		}
		return cached
	}
}

func discoverRelayHost(ctx context.Context, runner process.Runner) string {
	if runtime.GOOS != "linux" {
		return wildcardRelayHost
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
		return wildcardRelayHost
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
		return wildcardRelayHost
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
	return wildcardRelayHost
}
