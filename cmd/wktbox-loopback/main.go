package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wktbox/internal/interconnect"
	"wktbox/internal/loopback"
)

const (
	controlSocketPath = "/run/wktbox-loopback/control.sock"
	statusFilePath    = "/run/wktbox-loopback/status.json"
	dnsRouteTarget    = "192.0.2.1:53"
	dnsUpstream       = "127.0.0.11:53"
	dnsPort           = 53
)

type dependencies struct {
	request  func(context.Context, string) (loopback.Status, error)
	serve    func(context.Context) error
	dnsServe func(context.Context) error
	dnsProbe func(context.Context) error
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
	case "status", "sync":
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
		serve:    runSidecar,
		dnsServe: runDNSSidecar,
		dnsProbe: probeDNS,
	}
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
	daemon := loopback.NewDaemon(loopback.DaemonOptions{
		Source:     source,
		Reconciler: reconciler,
		Logf:       log.Printf,
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

type persistentController struct {
	loopback.Controller
	path string
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
