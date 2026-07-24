package loopback_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"wktbox/internal/lock"
	"wktbox/internal/loopback"
)

func TestDaemonInitialSyncAndDebouncesRelevantEvents(t *testing.T) {
	subscription := newFakeSubscription()
	source := &fakeSource{subscribers: []*fakeSubscription{subscription}}
	source.setContainers(containerWithPort("frontend", 5173))
	reconciler := newTestReconciler()
	daemon := loopback.NewDaemon(loopback.DaemonOptions{
		Source:         source,
		Reconciler:     reconciler,
		Debounce:       30 * time.Millisecond,
		ResyncInterval: time.Hour,
		Backoff: loopback.Backoff{
			Initial: 10 * time.Millisecond,
			Maximum: 20 * time.Millisecond,
		},
		Random: func() float64 { return 0.5 },
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := runDaemon(t, ctx, daemon)
	t.Cleanup(func() {
		cancel()
		waitDaemon(t, done)
	})
	waitFor(t, func() bool {
		return source.snapshotCount() == 1 &&
			daemon.Status().EventStream == loopback.EventStreamConnected
	})

	source.setContainers(containerWithPort("backend", 8000))
	subscription.events <- loopback.Event{Action: "start"}
	subscription.events <- loopback.Event{Action: "rename"}
	subscription.events <- loopback.Event{Action: "die"}

	waitFor(t, func() bool {
		return source.snapshotCount() == 2 &&
			hasRoute(daemon.Status(), 8000, loopback.RouteListening)
	})
	time.Sleep(60 * time.Millisecond)
	if got := source.snapshotCount(); got != 2 {
		t.Fatalf("snapshot calls = %d; want 2", got)
	}
}

func TestDaemonIgnoresIrrelevantEvents(t *testing.T) {
	subscription := newFakeSubscription()
	source := &fakeSource{subscribers: []*fakeSubscription{subscription}}
	source.setContainers(containerWithPort("frontend", 5173))
	daemon := loopback.NewDaemon(loopback.DaemonOptions{
		Source:         source,
		Reconciler:     newTestReconciler(),
		Debounce:       10 * time.Millisecond,
		ResyncInterval: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := runDaemon(t, ctx, daemon)
	t.Cleanup(func() {
		cancel()
		waitDaemon(t, done)
	})
	waitFor(t, func() bool { return source.snapshotCount() == 1 })

	subscription.events <- loopback.Event{Action: "attach"}
	time.Sleep(40 * time.Millisecond)

	if got := source.snapshotCount(); got != 1 {
		t.Fatalf("snapshot calls = %d; want 1", got)
	}
}

func TestDaemonReconnectsAndFullResyncsAfterStreamError(t *testing.T) {
	first := newFakeSubscription()
	second := newFakeSubscription()
	source := &fakeSource{subscribers: []*fakeSubscription{first, second}}
	source.setContainers(containerWithPort("frontend", 5173))
	daemon := loopback.NewDaemon(loopback.DaemonOptions{
		Source:         source,
		Reconciler:     newTestReconciler(),
		Debounce:       10 * time.Millisecond,
		ResyncInterval: time.Hour,
		Backoff: loopback.Backoff{
			Initial: 10 * time.Millisecond,
			Maximum: 20 * time.Millisecond,
			Jitter:  0.20,
		},
		Random: func() float64 { return 0.5 },
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := runDaemon(t, ctx, daemon)
	t.Cleanup(func() {
		cancel()
		waitDaemon(t, done)
	})
	waitFor(t, func() bool {
		return source.subscribeCount() == 1 && source.snapshotCount() == 1
	})

	source.setContainers(containerWithPort("backend", 8000))
	first.errors <- errors.New("stream lost")

	waitFor(t, func() bool {
		return source.subscribeCount() == 2 &&
			source.snapshotCount() == 2 &&
			hasRoute(daemon.Status(), 8000, loopback.RouteListening) &&
			daemon.Status().EventStream == loopback.EventStreamConnected
	})
}

func TestDaemonPeriodicResyncRecoversMissedEvent(t *testing.T) {
	subscription := newFakeSubscription()
	source := &fakeSource{subscribers: []*fakeSubscription{subscription}}
	source.setContainers(containerWithPort("frontend", 5173))
	daemon := loopback.NewDaemon(loopback.DaemonOptions{
		Source:         source,
		Reconciler:     newTestReconciler(),
		Debounce:       time.Hour,
		ResyncInterval: 20 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := runDaemon(t, ctx, daemon)
	t.Cleanup(func() {
		cancel()
		waitDaemon(t, done)
	})
	waitFor(t, func() bool { return source.snapshotCount() == 1 })
	source.setContainers(containerWithPort("database", 5432))

	waitFor(t, func() bool {
		return source.snapshotCount() >= 2 &&
			hasRoute(daemon.Status(), 5432, loopback.RouteListening)
	})
}

func TestDaemonBackgroundSyncWaitsForPortMappingTransaction(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "transaction.lock")
	unlock, err := lock.Acquire(context.Background(), lockPath)
	if err != nil {
		t.Fatal(err)
	}
	source := &fakeSource{}
	source.setContainers(containerWithPort("frontend", 5173))
	daemon := loopback.NewDaemon(loopback.DaemonOptions{
		Source:                  source,
		Reconciler:              newTestReconciler(),
		PortTransactionLockPath: lockPath,
	})

	result := make(chan error, 1)
	go func() {
		_, syncErr := daemon.Sync(context.Background())
		result <- syncErr
	}()

	time.Sleep(50 * time.Millisecond)
	if got := source.snapshotCount(); got != 0 {
		t.Fatalf("background sync read candidate config during transaction: snapshots = %d", got)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("background sync did not resume after transaction")
	}
	if got := source.snapshotCount(); got != 1 {
		t.Fatalf("snapshot calls = %d; want 1", got)
	}
}

func TestDaemonLimitsRepeatedErrorsByClass(t *testing.T) {
	source := &fakeSource{}
	reconciler := newTestReconciler()
	var mutex sync.Mutex
	var logs []string
	daemon := loopback.NewDaemon(loopback.DaemonOptions{
		Source:         source,
		Reconciler:     reconciler,
		ResyncInterval: time.Hour,
		Backoff: loopback.Backoff{
			Initial: 5 * time.Millisecond,
			Maximum: 5 * time.Millisecond,
			Jitter:  0.20,
		},
		Random: func() float64 { return 0.5 },
		Logf: func(format string, _ ...any) {
			mutex.Lock()
			defer mutex.Unlock()
			logs = append(logs, format)
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := runDaemon(t, ctx, daemon)
	waitFor(t, func() bool { return source.subscribeCount() >= 3 })
	cancel()
	waitDaemon(t, done)

	mutex.Lock()
	defer mutex.Unlock()
	if len(logs) != 1 {
		t.Fatalf("logs = %#v; want one throttled message", logs)
	}
}

type fakeSubscription struct {
	events chan loopback.Event
	errors chan error
}

func newFakeSubscription() *fakeSubscription {
	return &fakeSubscription{
		events: make(chan loopback.Event, 16),
		errors: make(chan error, 4),
	}
}

type fakeSource struct {
	mutex       sync.Mutex
	containers  []loopback.Container
	subscribers []*fakeSubscription
	snapshots   int
	subscribe   int
	closed      bool
	stopped     []string
}

func (source *fakeSource) Snapshot(context.Context) ([]loopback.Container, error) {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	source.snapshots++
	return append([]loopback.Container(nil), source.containers...), nil
}

func (source *fakeSource) Subscribe(context.Context) (loopback.Subscription, error) {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	index := source.subscribe
	source.subscribe++
	if index >= len(source.subscribers) {
		return loopback.Subscription{}, fmt.Errorf("subscription %d unavailable", index)
	}
	return loopback.Subscription{
		Events: source.subscribers[index].events,
		Errors: source.subscribers[index].errors,
	}, nil
}

func (source *fakeSource) Close() error {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	source.closed = true
	return nil
}

func (source *fakeSource) Stop(_ context.Context, id string) error {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	source.stopped = append(source.stopped, id)
	return nil
}

func (source *fakeSource) setContainers(containers ...loopback.Container) {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	source.containers = append([]loopback.Container(nil), containers...)
}

func (source *fakeSource) snapshotCount() int {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	return source.snapshots
}

func (source *fakeSource) subscribeCount() int {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	return source.subscribe
}

func containerWithPort(name string, port uint16) loopback.Container {
	return loopback.Container{
		ID:      name,
		Name:    name,
		Running: true,
		Ports: []loopback.PortBinding{{
			HostPort:  port,
			Protocol:  "tcp",
			Published: true,
		}},
	}
}

func newTestReconciler() *loopback.Reconciler {
	return loopback.NewReconciler(loopback.ReconcilerOptions{
		Listen: func(string, string) (net.Listener, error) {
			return &memoryListener{closed: make(chan struct{})}, nil
		},
	})
}

type memoryListener struct {
	once   sync.Once
	closed chan struct{}
}

func (listener *memoryListener) Accept() (net.Conn, error) {
	<-listener.closed
	return nil, net.ErrClosed
}

func (listener *memoryListener) Close() error {
	listener.once.Do(func() {
		close(listener.closed)
	})
	return nil
}

func (listener *memoryListener) Addr() net.Addr {
	return memoryAddr("loopback-test")
}

type memoryAddr string

func (address memoryAddr) Network() string { return "memory" }
func (address memoryAddr) String() string  { return string(address) }

func hasRoute(status loopback.Status, port uint16, state string) bool {
	for _, route := range status.Routes {
		if route.Port == port && route.State == state {
			return true
		}
	}
	return false
}

func runDaemon(t *testing.T, ctx context.Context, daemon *loopback.Daemon) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- daemon.Run(ctx)
	}()
	return done
}

func waitDaemon(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("daemon did not stop")
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition was not met")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
