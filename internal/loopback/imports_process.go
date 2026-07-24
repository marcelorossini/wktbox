package loopback

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"wktbox/internal/portforward"
)

type ProcessImportOptions struct {
	RelayAddress string
	TokenPath    string
	Executable   string
	StartTimeout time.Duration
}

type processImportFactory struct {
	options ProcessImportOptions
}

func NewProcessImportFactory(
	options ProcessImportOptions,
) (ImportProxyFactory, error) {
	if options.RelayAddress == "" {
		return nil, errors.New("import relay address is required")
	}
	if options.TokenPath == "" {
		return nil, errors.New("import relay token path is required")
	}
	if options.Executable == "" {
		executable, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolve loopback executable: %w", err)
		}
		options.Executable = executable
	}
	if options.StartTimeout <= 0 {
		options.StartTimeout = 5 * time.Second
	}
	return &processImportFactory{options: options}, nil
}

func (factory *processImportFactory) Probe(
	_ context.Context,
	spec ImportProxySpec,
) error {
	listeners, err := listenNamespaceLoopback(
		spec.Container.PID,
		spec.Mapping.TargetPort,
	)
	closeListeners(listeners)
	return err
}

func (factory *processImportFactory) Start(
	ctx context.Context,
	spec ImportProxySpec,
) (ImportProxy, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create import proxy readiness pipe: %w", err)
	}
	command := exec.Command(
		factory.options.Executable,
		"import-proxy",
		"--pid", strconv.Itoa(spec.Container.PID),
		"--port", strconv.Itoa(int(spec.Mapping.TargetPort)),
		"--relay", factory.options.RelayAddress,
		"--token", factory.options.TokenPath,
		"--mapping", spec.Mapping.Name,
		"--ready-fd", "3",
	)
	command.Stdin = nil
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.ExtraFiles = []*os.File{writer}
	if err := command.Start(); err != nil {
		reader.Close()
		writer.Close()
		return nil, fmt.Errorf("start import proxy: %w", err)
	}
	_ = writer.Close()
	ready := make(chan error, 1)
	go func() {
		defer reader.Close()
		var signal [1]byte
		_, readErr := io.ReadFull(reader, signal[:])
		if readErr == nil && signal[0] != 1 {
			readErr = fmt.Errorf("unexpected import proxy readiness byte %d", signal[0])
		}
		ready <- readErr
	}()
	timer := time.NewTimer(factory.options.StartTimeout)
	defer timer.Stop()
	select {
	case err := <-ready:
		if err != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			return nil, fmt.Errorf("wait for import proxy readiness: %w", err)
		}
	case <-ctx.Done():
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, ctx.Err()
	case <-timer.C:
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, errors.New("import proxy readiness timed out")
	}
	return &processImportProxy{command: command}, nil
}

type processImportProxy struct {
	command *exec.Cmd
	once    sync.Once
	err     error
}

func (proxy *processImportProxy) Close() error {
	proxy.once.Do(func() {
		if proxy.command == nil || proxy.command.Process == nil {
			return
		}
		killErr := proxy.command.Process.Kill()
		waitErr := proxy.command.Wait()
		if errors.Is(killErr, os.ErrProcessDone) {
			killErr = nil
		}
		var exitError *exec.ExitError
		if errors.As(waitErr, &exitError) {
			waitErr = nil
		}
		proxy.err = errors.Join(killErr, waitErr)
	})
	return proxy.err
}

type ImportProxyRunOptions struct {
	PID          int
	Port         uint16
	RelayAddress string
	Token        []byte
	MappingName  string
	Ready        io.Writer
}

func RunImportProxy(
	ctx context.Context,
	options ImportProxyRunOptions,
) error {
	if options.PID <= 0 {
		return fmt.Errorf("import proxy PID must be positive, got %d", options.PID)
	}
	if options.Port == 0 ||
		options.RelayAddress == "" ||
		len(options.Token) != portforward.TokenSize ||
		options.MappingName == "" {
		return errors.New("import proxy port, relay, token, and mapping are required")
	}
	listeners, err := listenNamespaceLoopback(options.PID, options.Port)
	if err != nil {
		return err
	}
	defer closeListeners(listeners)
	if options.Ready != nil {
		if _, err := options.Ready.Write([]byte{1}); err != nil {
			return fmt.Errorf("signal import proxy readiness: %w", err)
		}
	}
	failures := make(chan error, len(listeners))
	for _, listener := range listeners {
		go serveImportListener(ctx, listener, options, failures)
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-failures:
		return err
	}
}

func RunImportProxyArgs(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("import-proxy", flag.ContinueOnError)
	var pid int
	var port uint
	var relayAddress string
	var tokenPath string
	var mappingName string
	var readyFD int
	flags.IntVar(&pid, "pid", 0, "workload PID")
	flags.UintVar(&port, "port", 0, "workload localhost port")
	flags.StringVar(&relayAddress, "relay", "", "host relay address")
	flags.StringVar(&tokenPath, "token", "", "host relay token path")
	flags.StringVar(&mappingName, "mapping", "", "import mapping name")
	flags.IntVar(&readyFD, "ready-fd", -1, "readiness file descriptor")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 ||
		pid <= 0 ||
		port == 0 ||
		port > 65535 ||
		relayAddress == "" ||
		tokenPath == "" ||
		mappingName == "" {
		return errors.New("import proxy --pid, --port, --relay, --token, and --mapping are required")
	}
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		return fmt.Errorf("read import relay token: %w", err)
	}
	var ready io.Writer
	var readyFile *os.File
	if readyFD >= 0 {
		readyFile = os.NewFile(uintptr(readyFD), "import-proxy-ready")
		if readyFile == nil {
			return fmt.Errorf("open readiness file descriptor %d", readyFD)
		}
		defer readyFile.Close()
		ready = readyFile
	}
	return RunImportProxy(ctx, ImportProxyRunOptions{
		PID:          pid,
		Port:         uint16(port),
		RelayAddress: relayAddress,
		Token:        token,
		MappingName:  mappingName,
		Ready:        ready,
	})
}

func serveImportListener(
	ctx context.Context,
	listener net.Listener,
	options ImportProxyRunOptions,
	failures chan<- error,
) {
	for {
		source, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			select {
			case failures <- err:
			default:
			}
			return
		}
		go func() {
			defer source.Close()
			target, err := portforward.DialRelay(
				ctx,
				options.RelayAddress,
				options.Token,
				options.MappingName,
			)
			if err != nil {
				return
			}
			defer target.Close()
			portforward.BridgeTCP(source, target)
		}()
	}
}

func closeListeners(listeners []net.Listener) {
	for _, listener := range listeners {
		_ = listener.Close()
	}
}
