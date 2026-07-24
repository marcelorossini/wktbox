package loopback_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"wktbox/internal/loopback"
	"wktbox/internal/portforward"
)

func TestImportPreflightRejectsOneConflictWithoutStartingAnyProxy(t *testing.T) {
	factory := &fakeImportFactory{
		conflicts: map[string]error{
			"api/postgres": errors.New("address already in use"),
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
		"late/host-api": errors.New("address already in use"),
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
	return fakeImportProxy{
		close: func() {
			factory.closed = append(factory.closed, key)
		},
	}, nil
}

type fakeImportProxy struct {
	close func()
}

func (proxy fakeImportProxy) Close() error {
	proxy.close()
	return nil
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
