package loopback_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"wktbox/internal/loopback"
	"wktbox/internal/portforward"
)

func TestImportPreflightRejectsOneConflictWithoutStartingAnyProxy(t *testing.T) {
	factory := &fakeImportFactory{
		conflicts: map[string]error{
			"api/postgres": fmt.Errorf(
				"%w: address already in use",
				loopback.ErrImportPortConflict,
			),
		},
	}
	reconciler := loopback.NewImportReconciler(loopback.ImportOptions{
		Factory: factory,
	})
	workloads := []loopback.Container{{
		ID: "a", Name: "api", PID: 42, Running: true,
	}}
	err := reconciler.Preflight(
		context.Background(),
		workloads,
		[]portforward.Mapping{importMapping("postgres", 5432)},
	)
	if err == nil ||
		!strings.Contains(err.Error(), "api") ||
		!strings.Contains(err.Error(), "5432") {
		t.Fatalf("error = %v", err)
	}
	if len(factory.started) != 0 {
		t.Fatalf("started = %#v", factory.started)
	}
}

func TestImportProbeInfrastructureFailureDoesNotStopWorkloadOrReportConflict(
	t *testing.T,
) {
	factory := &fakeImportFactory{
		conflicts: map[string]error{
			"api/postgres": errors.New("open network namespace: permission denied"),
		},
	}
	stopper := &fakeImportStopper{}
	reconciler := loopback.NewImportReconciler(loopback.ImportOptions{
		Factory: factory,
		Stopper: stopper,
	})
	_, err := reconciler.Apply(
		context.Background(),
		[]loopback.Container{{
			ID: "a", Name: "api", PID: 42, Running: true,
		}},
		[]portforward.Mapping{importMapping("postgres", 5432)},
		loopback.ImportApplyEvent,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "permission denied") ||
		strings.Contains(err.Error(), "already uses") {
		t.Fatalf("error = %v", err)
	}
	if len(stopper.stopped) != 0 {
		t.Fatalf("stopped = %#v", stopper.stopped)
	}
}

func TestImportProbeSkipsWorkloadThatDisappearsAfterSnapshot(t *testing.T) {
	factory := &fakeImportFactory{
		conflicts: map[string]error{
			"api/postgres": fmt.Errorf(
				"%w: network namespace no longer exists",
				loopback.ErrImportWorkloadGone,
			),
		},
	}
	stopper := &fakeImportStopper{}
	reconciler := loopback.NewImportReconciler(loopback.ImportOptions{
		Factory: factory,
		Stopper: stopper,
	})
	_, err := reconciler.Apply(
		context.Background(),
		[]loopback.Container{{
			ID: "a", Name: "api", PID: 42, Running: true,
		}},
		[]portforward.Mapping{importMapping("postgres", 5432)},
		loopback.ImportApplyEvent,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(factory.started) != 0 || len(stopper.stopped) != 0 {
		t.Fatalf("factory=%#v stopped=%#v", factory, stopper.stopped)
	}
}

func TestImportApplyRollsBackNewProxiesWhenOneStartFails(t *testing.T) {
	factory := &fakeImportFactory{startError: map[string]error{
		"web/redis": errors.New("proxy failed"),
	}}
	reconciler := loopback.NewImportReconciler(loopback.ImportOptions{
		Factory: factory,
	})
	workloads := []loopback.Container{
		{ID: "a", Name: "api", PID: 42, Running: true},
		{ID: "b", Name: "web", PID: 43, Running: true},
	}
	mappings := []portforward.Mapping{
		importMapping("postgres", 5432),
		importMapping("redis", 6379),
	}

	_, err := reconciler.Apply(
		context.Background(),
		workloads,
		mappings,
		loopback.ImportApplyTransaction,
	)
	if err == nil || !strings.Contains(err.Error(), "web") {
		t.Fatalf("error = %v", err)
	}
	if len(factory.closed) == 0 {
		t.Fatalf("started proxies were not rolled back: %#v", factory)
	}
	if len(reconciler.Desired()) != 0 {
		t.Fatalf("desired = %#v", reconciler.Desired())
	}
}

func TestFutureConflictStopsWorkloadAndKeepsDesiredImport(t *testing.T) {
	factory := &fakeImportFactory{}
	stopper := &fakeImportStopper{}
	reconciler := loopback.NewImportReconciler(loopback.ImportOptions{
		Factory: factory,
		Stopper: stopper,
	})
	mapping := importMapping("host-api", 1234)
	if _, err := reconciler.Apply(
		context.Background(),
		nil,
		[]portforward.Mapping{mapping},
		loopback.ImportApplyTransaction,
	); err != nil {
		t.Fatal(err)
	}
	factory.conflicts = map[string]error{
		"late/host-api": fmt.Errorf(
			"%w: address already in use",
			loopback.ErrImportPortConflict,
		),
	}

	warnings, err := reconciler.Apply(
		context.Background(),
		[]loopback.Container{{
			ID: "late-id", Name: "late", PID: 77, Running: true,
		}},
		[]portforward.Mapping{mapping},
		loopback.ImportApplyEvent,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stopper.stopped, []string{"late-id"}) {
		t.Fatalf("stopped = %#v", stopper.stopped)
	}
	if len(warnings) != 1 ||
		warnings[0].Code != "port_import_conflict" ||
		warnings[0].Source != "late" ||
		warnings[0].Port != 1234 {
		t.Fatalf("warnings = %#v", warnings)
	}
	if got := reconciler.Desired(); len(got) != 1 || got[0].Name != "host-api" {
		t.Fatalf("desired = %#v", got)
	}

	warnings, err = reconciler.Apply(
		context.Background(),
		nil,
		[]portforward.Mapping{mapping},
		loopback.ImportApplyEvent,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || warnings[0].Code != "port_import_conflict" {
		t.Fatalf("conflict warning was not retained: %#v", warnings)
	}

	warnings, err = reconciler.Apply(
		context.Background(),
		nil,
		nil,
		loopback.ImportApplyEvent,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("removed import retained warnings: %#v", warnings)
	}
}

func TestImportApplyRestartsProxyThatExitedUnexpectedly(t *testing.T) {
	factory := &fakeImportFactory{}
	reconciler := loopback.NewImportReconciler(loopback.ImportOptions{
		Factory: factory,
	})
	workloads := []loopback.Container{{
		ID: "a", Name: "api", PID: 42, Running: true,
	}}
	mappings := []portforward.Mapping{importMapping("postgres", 5432)}

	if _, err := reconciler.Apply(
		context.Background(),
		workloads,
		mappings,
		loopback.ImportApplyEvent,
	); err != nil {
		t.Fatal(err)
	}
	factory.exit("api/postgres")
	if _, err := reconciler.Apply(
		context.Background(),
		workloads,
		mappings,
		loopback.ImportApplyEvent,
	); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(
		factory.started,
		[]string{"api/postgres", "api/postgres"},
	) {
		t.Fatalf("started proxies = %#v", factory.started)
	}
}

func TestImportStatusesReportUnexpectedProxyExit(t *testing.T) {
	factory := &fakeImportFactory{}
	reconciler := loopback.NewImportReconciler(loopback.ImportOptions{
		Factory: factory,
	})
	if _, err := reconciler.Apply(
		context.Background(),
		[]loopback.Container{{
			ID: "a", Name: "api", PID: 42, Running: true,
		}},
		[]portforward.Mapping{importMapping("postgres", 5432)},
		loopback.ImportApplyEvent,
	); err != nil {
		t.Fatal(err)
	}
	statuses := reconciler.Statuses()
	if len(statuses) != 1 ||
		statuses[0].Mapping != "postgres" ||
		statuses[0].Workload != "api" ||
		statuses[0].State != loopback.ImportListening {
		t.Fatalf("statuses = %#v", statuses)
	}

	factory.exit("api/postgres")
	statuses = reconciler.Statuses()
	if len(statuses) != 1 ||
		statuses[0].State != loopback.ImportExited ||
		statuses[0].Error == "" {
		t.Fatalf("statuses after exit = %#v", statuses)
	}
}

func TestImportStatusesRetainFailedProxyRestart(t *testing.T) {
	factory := &fakeImportFactory{}
	reconciler := loopback.NewImportReconciler(loopback.ImportOptions{
		Factory: factory,
	})
	workloads := []loopback.Container{{
		ID: "a", Name: "api", PID: 42, Running: true,
	}}
	mappings := []portforward.Mapping{importMapping("postgres", 5432)}
	if _, err := reconciler.Apply(
		context.Background(),
		workloads,
		mappings,
		loopback.ImportApplyEvent,
	); err != nil {
		t.Fatal(err)
	}
	factory.exit("api/postgres")
	factory.startError = map[string]error{
		"api/postgres": errors.New("restart process failed"),
	}

	if _, err := reconciler.Apply(
		context.Background(),
		workloads,
		mappings,
		loopback.ImportApplyEvent,
	); err == nil {
		t.Fatal("failed proxy restart unexpectedly succeeded")
	}
	statuses := reconciler.Statuses()
	if len(statuses) != 1 ||
		statuses[0].Mapping != "postgres" ||
		statuses[0].State != loopback.ImportFailed ||
		!strings.Contains(statuses[0].Error, "restart process failed") {
		t.Fatalf("statuses = %#v", statuses)
	}
}

func TestImportStatusesRetainEveryProxyRolledBackAfterBatchRestartFailure(
	t *testing.T,
) {
	factory := &fakeImportFactory{}
	reconciler := loopback.NewImportReconciler(loopback.ImportOptions{
		Factory: factory,
	})
	workloads := []loopback.Container{
		{ID: "a", Name: "api", PID: 42, Running: true},
		{ID: "b", Name: "worker", PID: 43, Running: true},
	}
	mappings := []portforward.Mapping{importMapping("postgres", 5432)}
	if _, err := reconciler.Apply(
		context.Background(),
		workloads,
		mappings,
		loopback.ImportApplyEvent,
	); err != nil {
		t.Fatal(err)
	}
	factory.exit("api/postgres")
	factory.exit("worker/postgres")
	factory.startError = map[string]error{
		"worker/postgres": errors.New("worker restart failed"),
	}

	if _, err := reconciler.Apply(
		context.Background(),
		workloads,
		mappings,
		loopback.ImportApplyEvent,
	); err == nil {
		t.Fatal("multi-workload proxy restart unexpectedly succeeded")
	}
	statuses := reconciler.Statuses()
	if len(statuses) != 2 {
		t.Fatalf("statuses = %#v", statuses)
	}
	if statuses[0].Workload != "api" ||
		statuses[0].State != loopback.ImportFailed ||
		!strings.Contains(statuses[0].Error, "rolled back") {
		t.Fatalf("rolled back proxy status = %#v", statuses[0])
	}
	if statuses[1].Workload != "worker" ||
		statuses[1].State != loopback.ImportFailed ||
		!strings.Contains(statuses[1].Error, "worker restart failed") {
		t.Fatalf("failed proxy status = %#v", statuses[1])
	}
}

func importMapping(name string, port uint16) portforward.Mapping {
	return portforward.Mapping{
		Name:          name,
		Direction:     portforward.Import,
		SourceAddress: "127.0.0.1",
		SourcePort:    port,
		TargetPort:    port,
	}
}

type fakeImportFactory struct {
	conflicts  map[string]error
	startError map[string]error
	started    []string
	closed     []string
	proxies    map[string]*fakeImportProxy
}

func (factory *fakeImportFactory) Probe(
	_ context.Context,
	spec loopback.ImportProxySpec,
) error {
	return factory.conflicts[specKey(spec)]
}

func (factory *fakeImportFactory) Start(
	_ context.Context,
	spec loopback.ImportProxySpec,
) (loopback.ImportProxy, error) {
	key := specKey(spec)
	if err := factory.startError[key]; err != nil {
		return nil, err
	}
	factory.started = append(factory.started, key)
	if factory.proxies == nil {
		factory.proxies = make(map[string]*fakeImportProxy)
	}
	proxy := &fakeImportProxy{
		done: make(chan struct{}),
		close: func() {
			factory.closed = append(factory.closed, key)
		},
	}
	factory.proxies[key] = proxy
	return proxy, nil
}

func (factory *fakeImportFactory) exit(key string) {
	factory.proxies[key].terminate()
}

type fakeImportProxy struct {
	done  chan struct{}
	close func()
	once  sync.Once
}

func (proxy *fakeImportProxy) Done() <-chan struct{} {
	return proxy.done
}

func (proxy *fakeImportProxy) Close() error {
	proxy.once.Do(func() {
		close(proxy.done)
		proxy.close()
	})
	return nil
}

func (proxy *fakeImportProxy) terminate() {
	proxy.once.Do(func() {
		close(proxy.done)
	})
}

func specKey(spec loopback.ImportProxySpec) string {
	return fmt.Sprintf("%s/%s", spec.Container.Name, spec.Mapping.Name)
}

type fakeImportStopper struct {
	stopped []string
}

func (stopper *fakeImportStopper) Stop(
	_ context.Context,
	containerID string,
) error {
	stopper.stopped = append(stopper.stopped, containerID)
	return nil
}
