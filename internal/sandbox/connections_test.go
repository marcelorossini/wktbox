package sandbox_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"wktbox/internal/compose"
	"wktbox/internal/lock"
	"wktbox/internal/ports"
	"wktbox/internal/sandbox"
	"wktbox/internal/state"
)

func TestConnectResolvesPrefixesPersistsAndAttachesBothServices(t *testing.T) {
	manager, backend, store := connectionManagerFixture(
		t,
		connectionBox("a4f8c9137d2b", "frontend", state.Ready, 23000),
		connectionBox("dfe31c662a91", "api", state.Ready, 23010),
	)

	got, err := manager.Connect(
		context.Background(),
		[]string{"a4f", "dfe"},
		"dev-stack",
		"dev",
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "dev-stack" ||
		got.Status != state.ConnectionReady ||
		len(got.Members) != 2 {
		t.Fatalf("connection = %#v", got)
	}
	current, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.Connections[got.ID].ID != got.ID {
		t.Fatalf("state = %#v", current.Connections)
	}
	if len(backend.connectCalls) != 4 {
		t.Fatalf("endpoint calls = %#v", backend.connectCalls)
	}
	for _, call := range backend.connectCalls {
		if call.Service == "docker" && call.Alias != call.BoxID+".wktbox" {
			t.Fatalf("docker endpoint = %#v", call)
		}
		if call.Service == "webtop" && call.Alias != "" {
			t.Fatalf("webtop endpoint = %#v", call)
		}
	}
}

func TestConnectRejectsAmbiguousOrStoppedMembers(t *testing.T) {
	manager, _, _ := connectionManagerFixture(
		t,
		connectionBox("a4f8c9137d2b", "api", state.Ready, 23000),
		connectionBox("a4f999999999", "api", state.Ready, 23010),
		connectionBox("dfe31c662a91", "worker", state.Stopped, 23020),
	)

	_, err := manager.Connect(
		context.Background(),
		[]string{"a4f", "dfe"},
		"",
		"dev",
	)
	if !errors.Is(err, sandbox.ErrSelectorAmbiguous) {
		t.Fatalf("ambiguous error = %v", err)
	}

	_, err = manager.Connect(
		context.Background(),
		[]string{"a4f8c9137d2b", "dfe"},
		"",
		"dev",
	)
	if err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("stopped error = %v", err)
	}
}

func TestConnectPersistsErrorForSafeRetry(t *testing.T) {
	manager, backend, store := connectionManagerFixture(
		t,
		connectionBox("a4f8c9137d2b", "frontend", state.Ready, 23000),
		connectionBox("dfe31c662a91", "api", state.Ready, 23010),
	)
	backend.connectionErr = errors.New("daemon unavailable")

	got, err := manager.Connect(
		context.Background(),
		[]string{"a4f", "dfe"},
		"",
		"dev",
	)
	if err == nil || !strings.Contains(err.Error(), "daemon unavailable") {
		t.Fatalf("error = %v", err)
	}
	if got.Status != state.ConnectionError {
		t.Fatalf("connection = %#v", got)
	}
	current, loadErr := store.Load(context.Background())
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if saved := current.Connections[got.ID]; saved.Status != state.ConnectionError ||
		saved.Error == "" {
		t.Fatalf("saved = %#v", saved)
	}
}

func TestDisconnectRemovesEndpointsNetworkAndState(t *testing.T) {
	manager, backend, store := connectionManagerFixture(
		t,
		connectionBox("a4f8c9137d2b", "frontend", state.Ready, 23000),
		connectionBox("dfe31c662a91", "api", state.Ready, 23010),
	)
	created, err := manager.Connect(
		context.Background(),
		[]string{"a4f", "dfe"},
		"dev-stack",
		"dev",
	)
	if err != nil {
		t.Fatal(err)
	}

	got, err := manager.Disconnect(context.Background(), "dev-stack")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID ||
		len(backend.disconnectCalls) != 4 ||
		len(backend.removeNetworkCalls) != 1 {
		t.Fatalf(
			"record=%#v disconnect=%#v remove=%#v",
			got,
			backend.disconnectCalls,
			backend.removeNetworkCalls,
		)
	}
	current, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Connections) != 0 {
		t.Fatalf("connections = %#v", current.Connections)
	}
}

func TestDestroyRemovesMemberAndDeletesConnectionBelowTwo(t *testing.T) {
	manager, backend, _ := connectionManagerFixture(
		t,
		connectionBox("a4f8c9137d2b", "frontend", state.Ready, 23000),
		connectionBox("dfe31c662a91", "api", state.Ready, 23010),
	)
	if _, err := manager.Connect(
		context.Background(),
		[]string{"a4f", "dfe"},
		"",
		"dev",
	); err != nil {
		t.Fatal(err)
	}

	if err := manager.Destroy(context.Background(), "a4f8c9137d2b"); err != nil {
		t.Fatal(err)
	}
	if len(backend.removeNetworkCalls) != 1 {
		t.Fatalf("network removals = %#v", backend.removeNetworkCalls)
	}
}

func TestRestartReconcilesConnectionWithPersistedVersion(t *testing.T) {
	manager, backend, _ := connectionManagerFixture(
		t,
		connectionBox("a4f8c9137d2b", "frontend", state.Ready, 23000),
		connectionBox("dfe31c662a91", "api", state.Ready, 23010),
	)
	if _, err := manager.Connect(
		context.Background(),
		[]string{"a4f", "dfe"},
		"",
		"1.2.3",
	); err != nil {
		t.Fatal(err)
	}
	backend.connectionNetworks = nil

	if err := manager.Restart(context.Background(), "a4f8c9137d2b"); err != nil {
		t.Fatal(err)
	}
	if len(backend.connectionNetworks) != 1 ||
		backend.connectionNetworks[0].Version != "1.2.3" {
		t.Fatalf("connection networks = %#v", backend.connectionNetworks)
	}
}

func TestDestroyRemovesOneMemberButPreservesLargerConnection(t *testing.T) {
	manager, backend, store := connectionManagerFixture(
		t,
		connectionBox("a4f8c9137d2b", "frontend", state.Ready, 23000),
		connectionBox("dfe31c662a91", "api", state.Ready, 23010),
		connectionBox("0a11ce55aa01", "worker", state.Ready, 23020),
	)
	created, err := manager.Connect(
		context.Background(),
		[]string{"a4f", "dfe", "0a1"},
		"dev-stack",
		"dev",
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := manager.Destroy(context.Background(), "0a11ce55aa01"); err != nil {
		t.Fatal(err)
	}
	current, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	remaining, exists := current.Connections[created.ID]
	if !exists ||
		len(remaining.Members) != 2 ||
		remaining.Members[0] != "a4f8c9137d2b" ||
		remaining.Members[1] != "dfe31c662a91" {
		t.Fatalf("remaining connection = %#v", remaining)
	}
	if len(backend.removeNetworkCalls) != 0 {
		t.Fatalf("network removals = %#v", backend.removeNetworkCalls)
	}
}

func connectionManagerFixture(
	t *testing.T,
	boxes ...state.BoxRecord,
) (sandbox.Manager, *fakeBackend, state.Store) {
	t.Helper()
	root := t.TempDir()
	store := state.NewStore(root)
	saveState(t, store, boxes...)
	managed := make([]compose.ManagedProject, 0, len(boxes))
	for _, box := range boxes {
		managed = append(managed, compose.ManagedProject{
			ID:          box.ID,
			Worktree:    box.Worktree,
			ProjectName: box.ProjectName,
			State:       compose.Running,
			Healthy:     box.Status == state.Ready,
			Ports:       box.Ports,
		})
	}
	backend := &fakeBackend{
		status:             readyComposeStatus(),
		managed:            managed,
		connectionStatuses: make(map[string]compose.ConnectionNetworkStatus),
	}
	now := func() time.Time {
		return time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	}
	return sandbox.NewManager(
		backend,
		store,
		lock.NewManager(root),
		now,
	), backend, store
}

func connectionBox(
	id string,
	name string,
	status state.Status,
	portStart int,
) state.BoxRecord {
	return state.BoxRecord{
		ID:             id,
		Name:           name,
		Worktree:       "/repo/" + name,
		ProjectName:    "wktbox-" + id,
		Status:         status,
		Ports:          ports.Block{Start: portStart, Size: 10},
		ComposePath:    "/state/" + id + "/compose.yml",
		SandboxEnvPath: "/state/" + id + "/sandbox.env",
	}
}
