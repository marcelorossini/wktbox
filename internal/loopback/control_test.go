package loopback_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"wktbox/internal/loopback"
	"wktbox/internal/portforward"
)

func TestControlSyncAndStatusUsePrivateSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "control.sock")
	controller := &fakeController{status: loopback.Status{
		EventStream: loopback.EventStreamConnected,
		Routes: []loopback.Route{{
			Port:     5173,
			Target:   "docker:5173",
			Sources:  []string{},
			Protocol: "tcp",
			State:    loopback.RouteListening,
		}},
		Warnings: []loopback.Warning{},
	}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- loopback.ServeControl(ctx, path, controller)
	}()
	waitForSocket(t, path)

	status, err := loopback.Request(context.Background(), path, "status")
	if err != nil {
		t.Fatal(err)
	}
	if controller.syncCalls != 0 || !reflect.DeepEqual(status, controller.status) {
		t.Fatalf("status=%#v calls=%d", status, controller.syncCalls)
	}
	status, err = loopback.Request(context.Background(), path, "sync")
	if err != nil {
		t.Fatal(err)
	}
	if controller.syncCalls != 1 || !reflect.DeepEqual(status, controller.status) {
		t.Fatalf("status=%#v calls=%d", status, controller.syncCalls)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %o; want 600", info.Mode().Perm())
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if parent.Mode().Perm() != 0o700 {
		t.Fatalf("parent mode = %o; want 700", parent.Mode().Perm())
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("socket still exists: %v", err)
	}
}

func TestControlRejectsUnknownCommand(t *testing.T) {
	path, cancel, done := startControl(t, &fakeController{})
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}()

	_, err := loopback.Request(context.Background(), path, "reload")

	if err == nil || !strings.Contains(err.Error(), `unknown loopback command "reload"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestControlReturnsSyncError(t *testing.T) {
	path, cancel, done := startControl(t, &fakeController{
		syncErr: errors.New("inner Docker unavailable"),
	})
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}()

	_, err := loopback.Request(context.Background(), path, "sync")

	if err == nil || !strings.Contains(err.Error(), "inner Docker unavailable") {
		t.Fatalf("error = %v", err)
	}
}

func TestControlPreflightsCandidateImportsWithoutApplying(t *testing.T) {
	controller := &fakeImportController{
		fakeController: &fakeController{
			status: loopback.Status{
				EventStream: loopback.EventStreamConnected,
			},
		},
	}
	path, cancel, done := startControl(t, controller)
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}()
	mappings := []portforward.Mapping{{
		Name:          "api",
		Direction:     portforward.Import,
		SourceAddress: "127.0.0.1",
		SourcePort:    1234,
		TargetPort:    1234,
	}}

	if err := loopback.RequestImportPreflight(
		context.Background(),
		path,
		mappings,
	); err != nil {
		t.Fatal(err)
	}
	if len(controller.preflightMappings) != 1 ||
		controller.preflightMappings[0].Name != "api" ||
		controller.applyCalls != 0 {
		t.Fatalf(
			"preflight=%#v apply=%d",
			controller.preflightMappings,
			controller.applyCalls,
		)
	}
}

func TestWriteStatusFileIsAtomicAndWorldReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "status.json")
	status := loopback.Status{
		EventStream: loopback.EventStreamConnected,
		Routes:      []loopback.Route{},
		Warnings:    []loopback.Warning{},
	}

	if err := loopback.WriteStatusFile(path, status); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded loopback.Status
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, status) {
		t.Fatalf("status = %#v; want %#v", decoded, status)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("status mode = %o; want 644", info.Mode().Perm())
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".status-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %#v", matches)
	}
}

type fakeController struct {
	mutex     sync.Mutex
	status    loopback.Status
	syncCalls int
	syncErr   error
}

type fakeImportController struct {
	*fakeController
	preflightMappings []portforward.Mapping
	applyCalls        int
}

func (controller *fakeImportController) PreflightImports(
	_ context.Context,
	mappings []portforward.Mapping,
) error {
	controller.preflightMappings = append(
		[]portforward.Mapping(nil),
		mappings...,
	)
	return nil
}

func (controller *fakeImportController) SyncImports(
	context.Context,
) (loopback.Status, error) {
	controller.applyCalls++
	return controller.status, nil
}

func (controller *fakeController) Sync(context.Context) (loopback.Status, error) {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	controller.syncCalls++
	return controller.status, controller.syncErr
}

func (controller *fakeController) Status() loopback.Status {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	return controller.status
}

func startControl(
	t *testing.T,
	controller loopback.Controller,
) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "control.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- loopback.ServeControl(ctx, path, controller)
	}()
	waitForSocket(t, path)
	return path, cancel, done
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("socket %s was not created", path)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
