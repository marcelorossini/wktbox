package loopback_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"wktbox/internal/loopback"
)

func TestReconcilerForwardsIPv4AndIPv6ToFreshDockerResolution(t *testing.T) {
	upstream := listenEcho(t)
	var mutex sync.Mutex
	var dialed []string
	reconciler := loopback.NewReconciler(loopback.ReconcilerOptions{
		Listen: net.Listen,
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			mutex.Lock()
			dialed = append(dialed, address)
			mutex.Unlock()
			return (&net.Dialer{}).DialContext(ctx, network, upstream.Addr().String())
		},
	})
	t.Cleanup(func() {
		shutdownReconciler(t, reconciler)
	})

	port := freeDualStackPort(t)
	status := reconciler.Apply(context.Background(), loopback.Desired{
		Publications: []loopback.Publication{{
			Port:    port,
			Target:  fmt.Sprintf("docker:%d", port),
			Sources: []string{"frontend"},
		}},
	})

	assertEcho(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))))
	assertEcho(t, net.JoinHostPort("::1", strconv.Itoa(int(port))))
	if len(status.Routes) != 1 || status.Routes[0].State != loopback.RouteListening {
		t.Fatalf("status = %#v", status)
	}
	mutex.Lock()
	defer mutex.Unlock()
	if len(dialed) != 2 || dialed[0] != fmt.Sprintf("docker:%d", port) ||
		dialed[1] != fmt.Sprintf("docker:%d", port) {
		t.Fatalf("dialed targets = %#v", dialed)
	}
}

func TestReconcilerRemovesBothListeners(t *testing.T) {
	reconciler := loopback.NewReconciler(loopback.ReconcilerOptions{
		Listen: net.Listen,
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, fmt.Errorf("not used")
		},
	})
	t.Cleanup(func() {
		shutdownReconciler(t, reconciler)
	})
	port := freeDualStackPort(t)
	reconciler.Apply(context.Background(), desiredPorts(port))

	status := reconciler.Apply(context.Background(), loopback.Desired{})

	if len(status.Routes) != 0 {
		t.Fatalf("routes = %#v", status.Routes)
	}
	assertEventuallyRefused(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))))
	assertEventuallyRefused(t, net.JoinHostPort("::1", strconv.Itoa(int(port))))
}

func TestReconcilerKeepsOtherRoutesWhenOnePortConflicts(t *testing.T) {
	occupied := listenTCP4(t, "127.0.0.1:0")
	occupiedPort := uint16(occupied.Addr().(*net.TCPAddr).Port)
	freePort := freeDualStackPort(t)
	reconciler := loopback.NewReconciler(loopback.ReconcilerOptions{
		Listen: net.Listen,
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, fmt.Errorf("not used")
		},
	})
	t.Cleanup(func() {
		shutdownReconciler(t, reconciler)
	})

	got := reconciler.Apply(context.Background(), desiredPorts(occupiedPort, freePort))

	assertRouteState(t, got, occupiedPort, loopback.RouteConflict)
	assertRouteState(t, got, freePort, loopback.RouteListening)
	if routeForPort(t, got, occupiedPort).Error == "" {
		t.Fatalf("conflict has no error: %#v", got.Routes)
	}
}

func TestReconcilerReusesListenerAndUpdatesSources(t *testing.T) {
	var mutex sync.Mutex
	listenCalls := 0
	reconciler := loopback.NewReconciler(loopback.ReconcilerOptions{
		Listen: func(network string, address string) (net.Listener, error) {
			mutex.Lock()
			listenCalls++
			mutex.Unlock()
			return net.Listen(network, address)
		},
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, fmt.Errorf("not used")
		},
	})
	t.Cleanup(func() {
		shutdownReconciler(t, reconciler)
	})
	port := freeDualStackPort(t)
	reconciler.Apply(context.Background(), desiredPorts(port))

	got := reconciler.Apply(context.Background(), loopback.Desired{
		Publications: []loopback.Publication{{
			Port:    port,
			Target:  fmt.Sprintf("docker:%d", port),
			Sources: []string{"api", "worker"},
		}},
	})

	mutex.Lock()
	defer mutex.Unlock()
	if listenCalls != 2 {
		t.Fatalf("listen calls = %d; want 2", listenCalls)
	}
	if fmt.Sprint(got.Routes[0].Sources) != "[api worker]" {
		t.Fatalf("sources = %#v", got.Routes[0].Sources)
	}
}

func TestReconcilerRetriesConflictOnNextApply(t *testing.T) {
	occupied := listenTCP4(t, "127.0.0.1:0")
	port := uint16(occupied.Addr().(*net.TCPAddr).Port)
	reconciler := loopback.NewReconciler(loopback.ReconcilerOptions{
		Listen: net.Listen,
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, fmt.Errorf("not used")
		},
	})
	t.Cleanup(func() {
		shutdownReconciler(t, reconciler)
	})
	assertRouteState(t, reconciler.Apply(context.Background(), desiredPorts(port)),
		port, loopback.RouteConflict)
	if err := occupied.Close(); err != nil {
		t.Fatal(err)
	}

	assertRouteState(t, reconciler.Apply(context.Background(), desiredPorts(port)),
		port, loopback.RouteListening)
}

func TestProxyTransparentlyForwardsHTTPAndHTTPS(t *testing.T) {
	for _, test := range []struct {
		name   string
		server func(http.Handler) *httptest.Server
		client func(*httptest.Server) *http.Client
		scheme string
	}{
		{
			name: "HTTP",
			server: func(handler http.Handler) *httptest.Server {
				return httptest.NewServer(handler)
			},
			client: func(*httptest.Server) *http.Client {
				return &http.Client{Timeout: 2 * time.Second}
			},
			scheme: "http",
		},
		{
			name: "HTTPS",
			server: func(handler http.Handler) *httptest.Server {
				return httptest.NewTLSServer(handler)
			},
			client: func(server *httptest.Server) *http.Client {
				client := server.Client()
				client.Timeout = 2 * time.Second
				return client
			},
			scheme: "https",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := test.server(http.HandlerFunc(func(
				response http.ResponseWriter,
				_ *http.Request,
			) {
				_, _ = response.Write([]byte("proxied"))
			}))
			defer upstream.Close()
			upstreamAddress := upstream.Listener.Addr().String()
			reconciler := loopback.NewReconciler(loopback.ReconcilerOptions{
				Listen: net.Listen,
				DialContext: func(
					ctx context.Context,
					network string,
					_ string,
				) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, network, upstreamAddress)
				},
			})
			t.Cleanup(func() {
				shutdownReconciler(t, reconciler)
			})
			port := freeDualStackPort(t)
			reconciler.Apply(context.Background(), desiredPorts(port))

			response, err := test.client(upstream).Get(fmt.Sprintf(
				"%s://127.0.0.1:%d",
				test.scheme,
				port,
			))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != "proxied" {
				t.Fatalf("body = %q", body)
			}
		})
	}
}

func TestProxyHandlesConcurrentConnections(t *testing.T) {
	upstream := listenEcho(t)
	reconciler := loopback.NewReconciler(loopback.ReconcilerOptions{
		Listen: net.Listen,
		DialContext: func(ctx context.Context, network string, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, upstream.Addr().String())
		},
	})
	t.Cleanup(func() {
		shutdownReconciler(t, reconciler)
	})
	port := freeDualStackPort(t)
	reconciler.Apply(context.Background(), desiredPorts(port))
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)))

	var group sync.WaitGroup
	errors := make(chan error, 20)
	for range 20 {
		group.Add(1)
		go func() {
			defer group.Done()
			connection, err := net.DialTimeout("tcp", address, 2*time.Second)
			if err != nil {
				errors <- err
				return
			}
			defer connection.Close()
			if _, err := connection.Write([]byte("concurrent")); err != nil {
				errors <- err
				return
			}
			buffer := make([]byte, len("concurrent"))
			if _, err := io.ReadFull(connection, buffer); err != nil {
				errors <- err
				return
			}
			if string(buffer) != "concurrent" {
				errors <- fmt.Errorf("echo = %q", buffer)
			}
		}()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestProxyPropagatesHalfClose(t *testing.T) {
	upstream := listenTCP4(t, "127.0.0.1:0")
	go func() {
		connection, err := upstream.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		request, _ := io.ReadAll(connection)
		_, _ = connection.Write([]byte("after-eof:" + string(request)))
	}()
	reconciler := loopback.NewReconciler(loopback.ReconcilerOptions{
		Listen: net.Listen,
		DialContext: func(ctx context.Context, network string, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, upstream.Addr().String())
		},
	})
	t.Cleanup(func() {
		shutdownReconciler(t, reconciler)
	})
	port := freeDualStackPort(t)
	reconciler.Apply(context.Background(), desiredPorts(port))
	connection, err := net.DialTCP(
		"tcp4",
		nil,
		&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: int(port)},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()

	if _, err := connection.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	if err := connection.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(connection)
	if err != nil {
		t.Fatal(err)
	}
	if string(response) != "after-eof:request" {
		t.Fatalf("response = %q", response)
	}
}

func desiredPorts(ports ...uint16) loopback.Desired {
	publications := make([]loopback.Publication, 0, len(ports))
	for _, port := range ports {
		publications = append(publications, loopback.Publication{
			Port:    port,
			Target:  fmt.Sprintf("docker:%d", port),
			Sources: []string{"fixture"},
		})
	}
	return loopback.Desired{Publications: publications}
}

func freeDualStackPort(t *testing.T) uint16 {
	t.Helper()
	listener := listenTCP4(t, "127.0.0.1:0")
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	probe, err := net.Listen("tcp6", net.JoinHostPort("::1", strconv.Itoa(int(port))))
	if err != nil {
		return freeDualStackPort(t)
	}
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func listenTCP4(t *testing.T, address string) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})
	return listener
}

func listenEcho(t *testing.T) net.Listener {
	t.Helper()
	listener := listenTCP4(t, "127.0.0.1:0")
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

func assertEcho(t *testing.T, address string) {
	t.Helper()
	connection, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Write([]byte("loopback")); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, len("loopback"))
	if _, err := io.ReadFull(connection, buffer); err != nil {
		t.Fatal(err)
	}
	if string(buffer) != "loopback" {
		t.Fatalf("echo = %q", buffer)
	}
}

func assertEventuallyRefused(t *testing.T, address string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err != nil {
			return
		}
		_ = connection.Close()
		if time.Now().After(deadline) {
			t.Fatalf("%s still accepts connections", address)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertRouteState(t *testing.T, status loopback.Status, port uint16, state string) {
	t.Helper()
	route := routeForPort(t, status, port)
	if route.State != state {
		t.Fatalf("route %d state = %q; want %q", port, route.State, state)
	}
}

func routeForPort(t *testing.T, status loopback.Status, port uint16) loopback.Route {
	t.Helper()
	for _, route := range status.Routes {
		if route.Port == port {
			return route
		}
	}
	t.Fatalf("route %d missing from %#v", port, status.Routes)
	return loopback.Route{}
}

func shutdownReconciler(t *testing.T, reconciler *loopback.Reconciler) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := reconciler.Close(ctx); err != nil {
		t.Fatal(err)
	}
}
