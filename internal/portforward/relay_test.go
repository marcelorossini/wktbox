package portforward_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"wktbox/internal/portforward"
)

func TestRelayAuthenticatesAndCopiesTCP(t *testing.T) {
	targetAddress := startEchoServer(t)
	token := bytes.Repeat([]byte{0x24}, portforward.TokenSize)
	relayAddress := startRelay(t, portforward.RelayOptions{
		Token: token,
		Resolve: func(name string) (string, bool) {
			return targetAddress, name == "echo"
		},
	})

	connection, err := portforward.DialRelay(
		context.Background(),
		relayAddress,
		token,
		"echo",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	body := make([]byte, 5)
	if _, err := io.ReadFull(connection, body); err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello" {
		t.Fatalf("echo = %q", body)
	}
}

func TestRelayRejectsInvalidTokenWithoutOpeningTarget(t *testing.T) {
	token := bytes.Repeat([]byte{0x24}, portforward.TokenSize)
	var opened atomic.Bool
	relayAddress := startRelay(t, portforward.RelayOptions{
		Token: token,
		Resolve: func(name string) (string, bool) {
			opened.Store(true)
			return "127.0.0.1:1", true
		},
	})

	_, err := portforward.DialRelay(
		context.Background(),
		relayAddress,
		bytes.Repeat([]byte{0xff}, portforward.TokenSize),
		"echo",
	)
	if err == nil {
		t.Fatal("expected authentication error")
	}
	if opened.Load() {
		t.Fatal("target was resolved before authentication")
	}
}

func TestRelayRejectsUnknownMapping(t *testing.T) {
	token := bytes.Repeat([]byte{0x24}, portforward.TokenSize)
	relayAddress := startRelay(t, portforward.RelayOptions{
		Token: token,
		Resolve: func(string) (string, bool) {
			return "", false
		},
	})

	_, err := portforward.DialRelay(
		context.Background(),
		relayAddress,
		token,
		"missing",
	)
	if err == nil {
		t.Fatal("expected unknown mapping error")
	}
}

func startRelay(t *testing.T, options portforward.RelayOptions) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	relay := portforward.NewRelay(options)
	ctx, cancel := context.WithCancel(context.Background())
	failures := make(chan error, 1)
	go func() {
		failures <- relay.Serve(ctx, listener)
	}()
	t.Cleanup(func() {
		cancel()
		_ = relay.Close()
		select {
		case err := <-failures:
			if err != nil && !errors.Is(err, context.Canceled) &&
				!errors.Is(err, net.ErrClosed) {
				t.Errorf("relay serve: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("relay did not stop")
		}
	})
	return listener.Addr().String()
}

func startEchoServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				_, _ = io.Copy(connection, connection)
			}()
		}
	}()
	return listener.Addr().String()
}
