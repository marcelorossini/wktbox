package portforward

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type RelayProcessOptions struct {
	ListenAddress string
	ConfigPath    string
	TokenPath     string
	PIDPath       string
	LogPath       string
	Executable    string
}

func RelayProcessPaths(configPath string) RelayProcessOptions {
	directory := filepath.Dir(configPath)
	return RelayProcessOptions{
		ConfigPath: configPath,
		TokenPath:  filepath.Join(directory, "port-relay.token"),
		PIDPath:    filepath.Join(directory, "port-relay.pid"),
		LogPath:    filepath.Join(directory, "port-relay.log"),
	}
}

func EnsureRelayToken(path string) ([]byte, error) {
	body, err := os.ReadFile(path)
	if err == nil {
		if len(body) != TokenSize {
			return nil, fmt.Errorf(
				"relay token %s contains %d bytes, want %d",
				path,
				len(body),
				TokenSize,
			)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return nil, fmt.Errorf("protect relay token: %w", err)
		}
		return body, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read relay token: %w", err)
	}
	token := make([]byte, TokenSize)
	if _, err := rand.Read(token); err != nil {
		return nil, fmt.Errorf("generate relay token: %w", err)
	}
	if err := writePrivateFile(path, token); err != nil {
		return nil, err
	}
	return token, nil
}

func RunRelayProcess(ctx context.Context, options RelayProcessOptions) error {
	if options.ListenAddress == "" ||
		options.ConfigPath == "" ||
		options.TokenPath == "" ||
		options.PIDPath == "" {
		return errors.New("relay listen address, config, token, and PID paths are required")
	}
	token, err := EnsureRelayToken(options.TokenPath)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", options.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen for host imports on %s: %w", options.ListenAddress, err)
	}
	pid := []byte(strconv.Itoa(os.Getpid()) + "\n")
	if err := writePrivateFile(options.PIDPath, pid); err != nil {
		listener.Close()
		return fmt.Errorf("write relay PID: %w", err)
	}
	defer os.Remove(options.PIDPath)
	relay := NewRelay(RelayOptions{
		Token: token,
		Resolve: func(name string) (string, bool) {
			return resolveImport(options.ConfigPath, name)
		},
	})
	err = relay.Serve(ctx, listener)
	if errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func RunRelayProcessArgs(
	ctx context.Context,
	arguments []string,
	stderr io.Writer,
) error {
	flags := flag.NewFlagSet("__port-relay", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var options RelayProcessOptions
	flags.StringVar(&options.ListenAddress, "listen", "", "relay listen address")
	flags.StringVar(&options.ConfigPath, "config", "", "port configuration path")
	flags.StringVar(&options.TokenPath, "token", "", "relay token path")
	flags.StringVar(&options.PIDPath, "pid", "", "relay PID path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected relay arguments: %v", flags.Args())
	}
	return RunRelayProcess(ctx, options)
}

func EnsureRelayProcess(
	ctx context.Context,
	options RelayProcessOptions,
) ([]byte, error) {
	token, err := EnsureRelayToken(options.TokenPath)
	if err != nil {
		return nil, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	probeErr := ProbeRelay(probeCtx, options.ListenAddress, token)
	cancel()
	if probeErr == nil {
		return token, nil
	}
	executable := options.Executable
	if executable == "" {
		executable, err = os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolve Wktbox executable: %w", err)
		}
	}
	if err := launchRelayProcess(executable, options); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		probeCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		probeErr = ProbeRelay(probeCtx, options.ListenAddress, token)
		cancel()
		if probeErr == nil {
			return token, nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return nil, fmt.Errorf(
		"host import relay %s did not become ready: %w",
		options.ListenAddress,
		probeErr,
	)
}

func StopRelayProcess(
	ctx context.Context,
	options RelayProcessOptions,
) error {
	token, err := os.ReadFile(options.TokenPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read relay token: %w", err)
	}
	stopCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := StopRelay(stopCtx, options.ListenAddress, token); err != nil {
		var operationError *net.OpError
		if errors.As(err, &operationError) {
			return nil
		}
		return err
	}
	return nil
}

func resolveImport(configPath string, name string) (string, bool) {
	mappings, err := LoadConfig(configPath)
	if err != nil {
		return "", false
	}
	for _, mapping := range mappings {
		if mapping.Direction == Import && mapping.Name == name {
			return net.JoinHostPort(
				mapping.SourceAddress,
				strconv.Itoa(int(mapping.SourcePort)),
			), true
		}
	}
	return "", false
}

func writePrivateFile(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create relay directory: %w", err)
	}
	temporary, err := os.CreateTemp(
		filepath.Dir(path),
		"."+filepath.Base(path)+".*.tmp",
	)
	if err != nil {
		return fmt.Errorf("create temporary relay file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("protect temporary relay file: %w", err)
	}
	if _, err := temporary.Write(body); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary relay file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary relay file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary relay file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace relay file: %w", err)
	}
	return nil
}
