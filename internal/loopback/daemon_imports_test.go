package loopback_test

import (
	"context"
	"reflect"
	"testing"

	"wktbox/internal/loopback"
	"wktbox/internal/portforward"
)

func TestDaemonSyncReconcilesImportsAsLifecycleEventsAndMergesWarnings(t *testing.T) {
	source := &fakeSource{containers: []loopback.Container{{
		ID: "api", Name: "api", PID: 42, Running: true,
	}}}
	imports := &fakeImportManager{warnings: []loopback.Warning{{
		Code: "port_import_conflict", Port: 1234, Source: "api",
	}}}
	mappings := []portforward.Mapping{importMapping("host-api", 1234)}
	daemon := loopback.NewDaemon(loopback.DaemonOptions{
		Source:     source,
		Reconciler: newTestReconciler(),
		Imports:    imports,
		LoadPortMappings: func() ([]portforward.Mapping, error) {
			return mappings, nil
		},
	})

	status, err := daemon.Sync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(imports.modes, []loopback.ImportApplyMode{
		loopback.ImportApplyEvent,
	}) {
		t.Fatalf("modes = %#v", imports.modes)
	}
	if len(status.Warnings) != 1 ||
		status.Warnings[0].Code != "port_import_conflict" {
		t.Fatalf("status = %#v", status)
	}
}

func TestDaemonSyncImportsUsesTransactionalMode(t *testing.T) {
	source := &fakeSource{containers: []loopback.Container{{
		ID: "api", Name: "api", PID: 42, Running: true,
	}}}
	imports := &fakeImportManager{}
	daemon := loopback.NewDaemon(loopback.DaemonOptions{
		Source:     source,
		Reconciler: newTestReconciler(),
		Imports:    imports,
		LoadPortMappings: func() ([]portforward.Mapping, error) {
			return []portforward.Mapping{importMapping("host-api", 1234)}, nil
		},
	})

	if _, err := daemon.SyncImports(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(imports.modes, []loopback.ImportApplyMode{
		loopback.ImportApplyTransaction,
	}) {
		t.Fatalf("modes = %#v", imports.modes)
	}
}

type fakeImportManager struct {
	modes    []loopback.ImportApplyMode
	warnings []loopback.Warning
}

func (manager *fakeImportManager) Apply(
	_ context.Context,
	_ []loopback.Container,
	_ []portforward.Mapping,
	mode loopback.ImportApplyMode,
) ([]loopback.Warning, error) {
	manager.modes = append(manager.modes, mode)
	return manager.warnings, nil
}

func (manager *fakeImportManager) Close() error {
	return nil
}
