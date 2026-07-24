package state_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"wktbox/internal/loopback"
	"wktbox/internal/portforward"
	"wktbox/internal/state"
)

func TestSaveIsAtomicAndRoundTrips(t *testing.T) {
	store := state.NewStore(t.TempDir())
	want := state.Empty()
	want.Boxes["abc"] = state.BoxRecord{
		ID:          "abc",
		Name:        "feature-auth",
		Worktree:    "/repo",
		ProjectName: "wktbox-abc",
		Status:      state.Ready,
		CreatedAt:   time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC),
		PortMappings: []portforward.Mapping{{
			Name:          "api",
			Direction:     portforward.Publish,
			SourceAddress: "127.0.0.1",
			SourcePort:    18000,
			TargetPort:    8000,
			CreatedAt:     time.Date(2026, 7, 23, 12, 1, 0, 0, time.UTC),
		}},
	}
	if err := store.Save(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("state = %#v, want %#v", got, want)
	}
	info, err := os.Stat(filepath.Join(store.Root(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("state permissions = %o", info.Mode().Perm())
	}
}

func TestLoadMissingReturnsEmptyVersionedState(t *testing.T) {
	got, err := state.NewStore(t.TempDir()).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 ||
		got.Boxes == nil || len(got.Boxes) != 0 ||
		got.Connections == nil || len(got.Connections) != 0 {
		t.Fatalf("state = %#v", got)
	}
}

func TestLoadVersionOneWithoutConnectionsInitializesMap(t *testing.T) {
	root := t.TempDir()
	body := []byte(`{"version":1,"boxes":{}}`)
	if err := os.WriteFile(filepath.Join(root, "state.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := state.NewStore(root).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Connections == nil || len(got.Connections) != 0 {
		t.Fatalf("connections = %#v", got.Connections)
	}
}

func TestLoadVersionOneWithoutPortMappingsKeepsEmptyCollection(t *testing.T) {
	root := t.TempDir()
	body := []byte(`{"version":1,"boxes":{"abc":{"id":"abc"}}}`)
	if err := os.WriteFile(filepath.Join(root, "state.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := state.NewStore(root).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Boxes["abc"].PortMappings != nil {
		t.Fatalf("port mappings = %#v", got.Boxes["abc"].PortMappings)
	}
}

func TestLoadRejectsCorruptState(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "state.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := state.NewStore(root).Load(context.Background())
	if !errors.Is(err, state.ErrCorrupt) {
		t.Fatalf("error = %v", err)
	}
}

func TestSaveHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := state.NewStore(t.TempDir()).Save(ctx, state.Empty())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestSaveDoesNotPersistTransientLoopbackStatus(t *testing.T) {
	root := t.TempDir()
	store := state.NewStore(root)
	current := state.Empty()
	current.Boxes["abc"] = state.BoxRecord{
		ID:          "abc",
		ProjectName: "wktbox-abc",
		Loopback: loopback.Status{
			EventStream: loopback.EventStreamConnected,
			Routes: []loopback.Route{{
				Port: 5173,
			}},
		},
	}

	if err := store.Save(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "loopback") ||
		strings.Contains(string(body), "eventStream") {
		t.Fatalf("transient loopback status was persisted:\n%s", body)
	}
}

func TestBoxDirRejectsUnsafeID(t *testing.T) {
	_, err := state.NewStore(t.TempDir()).BoxDir("../other")
	if !errors.Is(err, state.ErrInvalidBoxID) {
		t.Fatalf("error = %v", err)
	}
}
