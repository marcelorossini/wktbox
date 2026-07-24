package portforward_test

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"wktbox/internal/portforward"
)

func TestEnsureRelayTokenIsStableAndProtected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "port-relay.token")
	first, err := portforward.EnsureRelayToken(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := portforward.EnsureRelayToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != portforward.TokenSize || !bytes.Equal(first, second) {
		t.Fatalf("first=%x second=%x", first, second)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token mode = %o", info.Mode().Perm())
	}
}

func TestRunRelayProcessServesConfigAndStopsAuthenticated(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "ports.json")
	tokenPath := filepath.Join(root, "port-relay.token")
	pidPath := filepath.Join(root, "port-relay.pid")
	token, err := portforward.EnsureRelayToken(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	targetAddress := startEchoServer(t)
	targetHost, targetPort, err := net.SplitHostPort(targetAddress)
	if err != nil {
		t.Fatal(err)
	}
	portNumber := parseTestPort(t, targetPort)
	body, err := portforward.RenderConfig([]portforward.Mapping{{
		Name:          "echo",
		Direction:     portforward.Import,
		SourceAddress: targetHost,
		SourcePort:    portNumber,
		TargetPort:    1234,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, body, 0o600); err != nil {
		t.Fatal(err)
	}

	listenAddress := reserveTestAddress(t)
	done := make(chan error, 1)
	go func() {
		done <- portforward.RunRelayProcess(context.Background(), portforward.RelayProcessOptions{
			ListenAddress: listenAddress,
			ConfigPath:    configPath,
			TokenPath:     tokenPath,
			PIDPath:       pidPath,
		})
	}()
	waitForRelay(t, listenAddress, token)

	connection, err := portforward.DialRelay(
		context.Background(),
		listenAddress,
		token,
		"echo",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	echo := make([]byte, 2)
	if _, err := connection.Read(echo); err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	if string(echo) != "ok" {
		t.Fatalf("echo = %q", echo)
	}

	if err := portforward.StopRelay(context.Background(), listenAddress, token); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("relay process: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("relay process did not stop")
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid file remained: %v", err)
	}
}

func reserveTestAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func waitForRelay(t *testing.T, address string, token []byte) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		err := portforward.ProbeRelay(ctx, address, token)
		cancel()
		if err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("relay %s did not become ready", address)
}

func parseTestPort(t *testing.T, value string) uint16 {
	t.Helper()
	mappings, err := portforward.ParseImports([]string{value}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return mappings[0].SourcePort
}
