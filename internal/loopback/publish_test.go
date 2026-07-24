package loopback_test

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"wktbox/internal/loopback"
)

func TestPublishServerForwardsConfiguredTCPRoute(t *testing.T) {
	target := startPublishEchoServer(t)
	server := loopback.NewPublishServer([]loopback.PublishRoute{{
		Name:          "echo",
		ListenAddress: "127.0.0.1:0",
		TargetAddress: target.Addr().String(),
	}})
	listeners, err := server.Listen(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx, listeners)
	}()
	t.Cleanup(func() {
		cancel()
		_ = server.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("serve publication: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("publication server did not stop")
		}
	})

	connection, err := net.DialTimeout(
		"tcp",
		listeners[0].Addr().String(),
		time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Write([]byte("published")); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len("published"))
	if _, err := io.ReadFull(connection, response); err != nil {
		t.Fatal(err)
	}
	if string(response) != "published" {
		t.Fatalf("response = %q", response)
	}
}

func TestPublishServerClosesEarlierListenersWhenBatchBindFails(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()

	server := loopback.NewPublishServer([]loopback.PublishRoute{
		{Name: "free", ListenAddress: "127.0.0.1:0", TargetAddress: "127.0.0.1:1"},
		{Name: "conflict", ListenAddress: occupied.Addr().String(), TargetAddress: "127.0.0.1:1"},
	})
	if _, err := server.Listen(context.Background()); err == nil {
		t.Fatal("expected batch bind conflict")
	}
	if server.ListenerCount() != 0 {
		t.Fatalf("listeners remain active: %d", server.ListenerCount())
	}
}

func startPublishEchoServer(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})
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
	return listener
}
