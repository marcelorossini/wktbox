package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"wktbox/internal/interconnect"
	"wktbox/internal/loopback"
	"wktbox/internal/portforward"
)

const (
	controlSocketPath = "/run/wktbox-loopback/control.sock"
	statusFilePath    = "/run/wktbox-loopback/status.json"
	dnsRouteTarget    = "192.0.2.1:53"
	dnsUpstream       = "127.0.0.11:53"
	dnsPort           = 53
)

type dependencies struct {
	request      func(context.Context, string) (loopback.Status, error)
	preflight    func(context.Context, []portforward.Mapping) error
	serve        func(context.Context) error
	publishServe func(context.Context) error
	dnsServe     func(context.Context) error
	dnsProbe     func(context.Context) error
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	os.Exit(execute(ctx, os.Args[1:], os.Stdout, os.Stderr, defaultDependencies()))
}

func execute(
	ctx context.Context,
	arguments []string,
	stdout io.Writer,
	stderr io.Writer,
	deps dependencies,
) int {
	if len(arguments) == 0 || arguments[0] == "--help" || arguments[0] == "-h" {
		printUsage(stdout)
		return 0
	}
	command := arguments[0]
	switch command {
	case "serve":
		if len(arguments) != 1 {
			printUsage(stderr)
			return 2
		}
		if deps.serve == nil {
			deps.serve = runSidecar
		}
		if err := deps.serve(ctx); err != nil {
			fmt.Fprintln(stderr, "wktbox-loopback:", err)
			return 1
		}
		return 0
	case "import-proxy":
		if err := loopback.RunImportProxyArgs(ctx, arguments[1:]); err != nil {
			fmt.Fprintln(stderr, "wktbox-loopback:", err)
			return 1
		}
		return 0
	case "publish-serve":
		if len(arguments) != 1 {
			printUsage(stderr)
			return 2
		}
		if deps.publishServe == nil {
			deps.publishServe = runPublishSidecar
		}
		if err := deps.publishServe(ctx); err != nil {
			fmt.Fprintln(stderr, "wktbox-loopback:", err)
			return 1
		}
		return 0
	case "imports-preflight":
		if len(arguments) != 3 || arguments[1] != "--mappings" {
			printUsage(stderr)
			return 2
		}
		body, err := base64.RawURLEncoding.DecodeString(arguments[2])
		if err != nil {
			fmt.Fprintln(stderr, "wktbox-loopback: decode import mappings:", err)
			return 2
		}
		var mappings []portforward.Mapping
		if err := json.Unmarshal(body, &mappings); err != nil {
			fmt.Fprintln(stderr, "wktbox-loopback: decode import mappings:", err)
			return 2
		}
		if err := portforward.ValidateSet(mappings); err != nil {
			fmt.Fprintln(stderr, "wktbox-loopback:", err)
			return 2
		}
		if deps.preflight == nil {
			deps.preflight = func(
				requestContext context.Context,
				requested []portforward.Mapping,
			) error {
				return loopback.RequestImportPreflight(
					requestContext,
					controlSocketPath,
					requested,
				)
			}
		}
		if err := deps.preflight(ctx, mappings); err != nil {
			fmt.Fprintln(stderr, "wktbox-loopback:", err)
			return 1
		}
		return 0
	case "dns-serve":
		if len(arguments) != 1 {
			printUsage(stderr)
			return 2
		}
		if deps.dnsServe == nil {
			deps.dnsServe = runDNSSidecar
		}
		if err := deps.dnsServe(ctx); err != nil {
			fmt.Fprintln(stderr, "wktbox-loopback:", err)
			return 1
		}
		return 0
	case "dns-probe":
		if len(arguments) != 1 {
			printUsage(stderr)
			return 2
		}
		if deps.dnsProbe == nil {
			deps.dnsProbe = probeDNS
		}
		if err := deps.dnsProbe(ctx); err != nil {
			fmt.Fprintln(stderr, "wktbox-loopback:", err)
			return 1
		}
		return 0
	case "status", "sync", "imports-apply":
		jsonOutput, valid := parseOutputFlags(arguments[1:])
		if !valid {
			printUsage(stderr)
			return 2
		}
		if deps.request == nil {
			deps.request = func(requestContext context.Context, requested string) (loopback.Status, error) {
				return loopback.Request(requestContext, controlSocketPath, requested)
			}
		}
		status, err := deps.request(ctx, command)
		if err != nil {
			fmt.Fprintln(stderr, "wktbox-loopback:", err)
			return 1
		}
		if jsonOutput {
			if err := json.NewEncoder(stdout).Encode(status); err != nil {
				fmt.Fprintln(stderr, "wktbox-loopback:", err)
				return 1
			}
		} else {
			printStatus(stdout, status)
		}
		if status.EventStream != loopback.EventStreamConnected {
			fmt.Fprintf(stderr, "wktbox-loopback: event stream is %s\n", status.EventStream)
			return 1
		}
		return 0
	default:
		printUsage(stderr)
		return 2
	}
}

func defaultDependencies() dependencies {
	return dependencies{
		request: func(ctx context.Context, command string) (loopback.Status, error) {
			return loopback.Request(ctx, controlSocketPath, command)
		},
		preflight: func(
			ctx context.Context,
			mappings []portforward.Mapping,
		) error {
			return loopback.RequestImportPreflight(
				ctx,
				controlSocketPath,
				mappings,
			)
		},
		serve:        runSidecar,
		publishServe: runPublishSidecar,
		dnsServe:     runDNSSidecar,
		dnsProbe:     probeDNS,
	}
}

func runPublishSidecar(ctx context.Context) error {
	configPath := os.Getenv("WKTBOX_PORT_CONFIG")
	if configPath == "" {
		return errors.New("WKTBOX_PORT_CONFIG is required")
	}
	mappings, err := portforward.LoadConfig(configPath)
	if err != nil {
		return err
	}
	return loopback.RunPublishServer(ctx, mappings, "docker")
}

func runDNSSidecar(ctx context.Context) error {
	listenAddress, err := interconnect.LocalAddressForRoute(
		dnsRouteTarget,
		dnsPort,
	)
	if err != nil {
		return err
	}
	return interconnect.ServeDNS(ctx, interconnect.DNSOptions{
		ListenAddress:   listenAddress,
		UpstreamAddress: dnsUpstream,
		Timeout:         3 * time.Second,
	})
}

func probeDNS(ctx context.Context) error {
	serverAddress, err := interconnect.LocalAddressForRoute(
		dnsRouteTarget,
		dnsPort,
	)
	if err != nil {
		return err
	}
	return interconnect.ProbeDNS(ctx, serverAddress)
}

func runSidecar(ctx context.Context) error {
	source, err := loopback.NewDockerSourceFromEnv()
	if err != nil {
		return err
	}
	reconciler := loopback.NewReconciler(loopback.ReconcilerOptions{})
	importFactory, loadMappings, err := importRuntimeFromEnv()
	if err != nil {
		source.Close()
		return err
	}
	imports := loopback.NewImportReconciler(loopback.ImportOptions{
		Factory: importFactory,
		Stopper: source,
	})
	daemon := loopback.NewDaemon(loopback.DaemonOptions{
		Source:           source,
		Reconciler:       reconciler,
		Imports:          imports,
		LoadPortMappings: loadMappings,
		Logf:             log.Printf,
	})
	controller := persistentController{
		Controller: daemon,
		path:       statusFilePath,
	}
	if err := loopback.WriteStatusFile(statusFilePath, daemon.Status()); err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	go func() {
		results <- loopback.ServeControl(runCtx, controlSocketPath, controller)
	}()
	go func() {
		results <- daemon.Run(runCtx)
	}()
	go persistStatus(runCtx, daemon, statusFilePath)

	first := <-results
	cancel()
	second := <-results
	return errors.Join(first, second)
}

func importRuntimeFromEnv() (
	loopback.ImportProxyFactory,
	func() ([]portforward.Mapping, error),
	error,
) {
	configPath := os.Getenv("WKTBOX_PORT_CONFIG")
	tokenPath := os.Getenv("WKTBOX_RELAY_TOKEN")
	relayHost := os.Getenv("WKTBOX_RELAY_HOST")
	relayPort := os.Getenv("WKTBOX_RELAY_PORT")
	if configPath == "" || tokenPath == "" || relayHost == "" || relayPort == "" {
		return nil, nil, errors.New(
			"WKTBOX_PORT_CONFIG, WKTBOX_RELAY_TOKEN, WKTBOX_RELAY_HOST, and WKTBOX_RELAY_PORT are required",
		)
	}
	var routeReader io.Reader
	var routeFile *os.File
	if relayHost == "host-gateway" {
		routeFile, _ = os.Open("/proc/net/route")
		if routeFile != nil {
			defer routeFile.Close()
			routeReader = routeFile
		}
	}
	relayIPv4, err := resolveRelayIPv4(relayHost, net.LookupHost, routeReader)
	if err != nil {
		return nil, nil, err
	}
	relayAddress := net.JoinHostPort(relayIPv4, relayPort)
	factory, err := loopback.NewProcessImportFactory(loopback.ProcessImportOptions{
		RelayAddress: relayAddress,
		TokenPath:    tokenPath,
	})
	if err != nil {
		return nil, nil, err
	}
	return factory, func() ([]portforward.Mapping, error) {
		return portforward.LoadConfig(configPath)
	}, nil
}

func resolveRelayIPv4(
	host string,
	lookup func(string) ([]string, error),
	linuxRoutes io.Reader,
) (string, error) {
	lookupHost := host
	if host == "host-gateway" {
		lookupHost = "host.docker.internal"
	}
	addresses, lookupErr := lookup(lookupHost)
	for _, address := range addresses {
		if parsed := net.ParseIP(address); parsed != nil && parsed.To4() != nil {
			return parsed.String(), nil
		}
	}
	if host != "host-gateway" {
		if lookupErr != nil {
			return "", fmt.Errorf("resolve host relay %s: %w", host, lookupErr)
		}
		return "", fmt.Errorf("host relay %s has no IPv4 address", host)
	}

	gateway, routeErr := linuxDefaultGateway(linuxRoutes)
	if routeErr == nil {
		return gateway, nil
	}
	if lookupErr != nil {
		return "", fmt.Errorf(
			"resolve host relay: host.docker.internal: %v; default gateway: %w",
			lookupErr,
			routeErr,
		)
	}
	return "", fmt.Errorf(
		"resolve host relay: host.docker.internal has no IPv4 address; default gateway: %w",
		routeErr,
	)
}

func linuxDefaultGateway(routes io.Reader) (string, error) {
	if routes == nil {
		return "", errors.New("/proc/net/route is unavailable")
	}
	scanner := bufio.NewScanner(routes)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 || fields[1] != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(fields[3], 16, 32)
		if err != nil || flags&0x3 != 0x3 {
			continue
		}
		gateway, err := strconv.ParseUint(fields[2], 16, 32)
		if err != nil || gateway == 0 {
			continue
		}
		return net.IPv4(
			byte(gateway),
			byte(gateway>>8),
			byte(gateway>>16),
			byte(gateway>>24),
		).String(), nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read /proc/net/route: %w", err)
	}
	return "", errors.New("IPv4 default gateway not found")
}

type persistentController struct {
	loopback.Controller
	path string
}

func (controller persistentController) SyncImports(
	ctx context.Context,
) (loopback.Status, error) {
	importController, ok := controller.Controller.(loopback.ImportController)
	if !ok {
		return loopback.Status{}, errors.New(
			"loopback controller does not support port imports",
		)
	}
	status, err := importController.SyncImports(ctx)
	if err != nil {
		return status, err
	}
	if err := loopback.WriteStatusFile(controller.path, status); err != nil {
		return status, err
	}
	return status, nil
}

func (controller persistentController) PreflightImports(
	ctx context.Context,
	mappings []portforward.Mapping,
) error {
	importController, ok := controller.Controller.(loopback.ImportController)
	if !ok {
		return errors.New("loopback controller does not support port imports")
	}
	return importController.PreflightImports(ctx, mappings)
}

func (controller persistentController) Sync(ctx context.Context) (loopback.Status, error) {
	status, err := controller.Controller.Sync(ctx)
	if err != nil {
		return status, err
	}
	if err := loopback.WriteStatusFile(controller.path, status); err != nil {
		return status, err
	}
	return status, nil
}

func persistStatus(
	ctx context.Context,
	controller loopback.Controller,
	path string,
) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := loopback.WriteStatusFile(path, controller.Status()); err != nil {
				log.Printf("persist loopback status: %v", err)
			}
		}
	}
}

func parseOutputFlags(arguments []string) (bool, bool) {
	if len(arguments) == 0 {
		return false, true
	}
	if len(arguments) == 1 && arguments[0] == "--json" {
		return true, true
	}
	return false, false
}

func printUsage(destination io.Writer) {
	fmt.Fprintln(destination, "Usage: wktbox-loopback serve")
	fmt.Fprintln(destination, "       wktbox-loopback publish-serve")
	fmt.Fprintln(destination, "       wktbox-loopback imports-preflight --mappings <base64url>")
	fmt.Fprintln(destination, "       wktbox-loopback imports-apply [--json]")
	fmt.Fprintln(destination, "       wktbox-loopback dns-serve")
	fmt.Fprintln(destination, "       wktbox-loopback dns-probe")
	fmt.Fprintln(destination, "       wktbox-loopback sync [--json]")
	fmt.Fprintln(destination, "       wktbox-loopback status [--json]")
}

func printStatus(destination io.Writer, status loopback.Status) {
	fmt.Fprintf(destination, "Event stream: %s\n", status.EventStream)
	for _, route := range status.Routes {
		fmt.Fprintf(destination, "localhost:%d -> %s (%s)\n", route.Port, route.Target, route.State)
	}
	for _, warning := range status.Warnings {
		fmt.Fprintf(destination, "warning: %s\n", warning.Message)
	}
}
