package sandbox_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"wktbox/internal/compose"
	"wktbox/internal/lock"
	"wktbox/internal/loopback"
	"wktbox/internal/portforward"
	"wktbox/internal/ports"
	"wktbox/internal/sandbox"
	"wktbox/internal/state"
)

type fakeBackend struct {
	status             compose.Status
	managed            []compose.ManagedProject
	upCalls            []compose.Project
	startCalls         []compose.Project
	stopCalls          []compose.Project
	restartCalls       []compose.Project
	downCalls          []downCall
	upErr              error
	err                error
	loopback           loopback.Status
	loopbackErr        error
	statusCalls        []compose.Project
	syncCalls          []compose.Project
	connectionNetworks []compose.ConnectionNetwork
	connectionStatuses map[string]compose.ConnectionNetworkStatus
	connectCalls       []compose.ConnectionEndpoint
	disconnectCalls    []compose.ConnectionEndpoint
	removeNetworkCalls []string
	connectionErr      error
	portPreflightErr   error
	portApplyErr       error
	portApplyErrors    []error
	portRuntimeErrors  []error
	portPreflightCalls [][]portforward.Mapping
	portApplyCalls     []compose.Project
	portRuntimeCalls   []compose.Project
	portApplyHook      func(context.Context, compose.Project) error
}

type downCall struct {
	project compose.Project
	volumes bool
}

func (backend *fakeBackend) Up(_ context.Context, project compose.Project) error {
	backend.upCalls = append(backend.upCalls, project)
	if backend.upErr != nil {
		return backend.upErr
	}
	return backend.err
}

func (backend *fakeBackend) Start(_ context.Context, project compose.Project) error {
	backend.startCalls = append(backend.startCalls, project)
	return backend.err
}

func (backend *fakeBackend) Stop(_ context.Context, project compose.Project) error {
	backend.stopCalls = append(backend.stopCalls, project)
	return backend.err
}

func (backend *fakeBackend) Restart(_ context.Context, project compose.Project) error {
	backend.restartCalls = append(backend.restartCalls, project)
	return backend.err
}

func (backend *fakeBackend) Down(
	_ context.Context,
	project compose.Project,
	volumes bool,
) error {
	backend.downCalls = append(backend.downCalls, downCall{project: project, volumes: volumes})
	return backend.err
}

func (backend *fakeBackend) Inspect(
	context.Context,
	compose.Project,
) (compose.Status, error) {
	return backend.status, backend.err
}

func (backend *fakeBackend) ListManaged(context.Context) ([]compose.ManagedProject, error) {
	return backend.managed, backend.err
}

func (backend *fakeBackend) LoopbackStatus(
	_ context.Context,
	project compose.Project,
) (loopback.Status, error) {
	backend.statusCalls = append(backend.statusCalls, project)
	return backend.loopback, backend.loopbackErr
}

func (backend *fakeBackend) LoopbackSync(
	_ context.Context,
	project compose.Project,
) (loopback.Status, error) {
	backend.syncCalls = append(backend.syncCalls, project)
	return backend.loopback, backend.loopbackErr
}

func (backend *fakeBackend) PortImportPreflight(
	_ context.Context,
	_ compose.Project,
	mappings []portforward.Mapping,
) error {
	backend.portPreflightCalls = append(
		backend.portPreflightCalls,
		append([]portforward.Mapping(nil), mappings...),
	)
	return backend.portPreflightErr
}

func (backend *fakeBackend) PortImportApply(
	ctx context.Context,
	project compose.Project,
) (loopback.Status, error) {
	backend.portApplyCalls = append(backend.portApplyCalls, project)
	if backend.portApplyHook != nil {
		if err := backend.portApplyHook(ctx, project); err != nil {
			return backend.loopback, err
		}
	}
	index := len(backend.portApplyCalls) - 1
	if index < len(backend.portApplyErrors) {
		return backend.loopback, backend.portApplyErrors[index]
	}
	return backend.loopback, backend.portApplyErr
}

func (backend *fakeBackend) PortRuntimeApply(
	_ context.Context,
	project compose.Project,
) error {
	backend.portRuntimeCalls = append(backend.portRuntimeCalls, project)
	index := len(backend.portRuntimeCalls) - 1
	if index < len(backend.portRuntimeErrors) {
		return backend.portRuntimeErrors[index]
	}
	return nil
}

func (backend *fakeBackend) EnsureConnectionNetwork(
	_ context.Context,
	network compose.ConnectionNetwork,
) error {
	backend.connectionNetworks = append(backend.connectionNetworks, network)
	if backend.connectionErr != nil {
		return backend.connectionErr
	}
	if backend.connectionStatuses == nil {
		backend.connectionStatuses = make(map[string]compose.ConnectionNetworkStatus)
	}
	status := backend.connectionStatuses[network.DockerName]
	status.Exists = true
	status.Managed = true
	status.ConnectionID = network.ID
	if status.Endpoints == nil {
		status.Endpoints = make(map[string]string)
	}
	backend.connectionStatuses[network.DockerName] = status
	return nil
}

func (backend *fakeBackend) InspectConnectionNetwork(
	_ context.Context,
	name string,
) (compose.ConnectionNetworkStatus, error) {
	if backend.connectionErr != nil {
		return compose.ConnectionNetworkStatus{}, backend.connectionErr
	}
	status := backend.connectionStatuses[name]
	if status.Endpoints == nil {
		status.Endpoints = make(map[string]string)
	}
	return status, nil
}

func (backend *fakeBackend) ConnectConnectionEndpoint(
	_ context.Context,
	endpoint compose.ConnectionEndpoint,
) error {
	backend.connectCalls = append(backend.connectCalls, endpoint)
	if backend.connectionErr != nil {
		return backend.connectionErr
	}
	status := backend.connectionStatuses[endpoint.Network]
	if status.Endpoints == nil {
		status.Endpoints = make(map[string]string)
	}
	status.Endpoints[endpoint.BoxID+"/"+endpoint.Service] =
		endpoint.BoxID + "-" + endpoint.Service
	backend.connectionStatuses[endpoint.Network] = status
	return nil
}

func (backend *fakeBackend) DisconnectConnectionEndpoint(
	_ context.Context,
	endpoint compose.ConnectionEndpoint,
) error {
	backend.disconnectCalls = append(backend.disconnectCalls, endpoint)
	if backend.connectionErr != nil {
		return backend.connectionErr
	}
	status := backend.connectionStatuses[endpoint.Network]
	delete(status.Endpoints, endpoint.BoxID+"/"+endpoint.Service)
	backend.connectionStatuses[endpoint.Network] = status
	return nil
}

func (backend *fakeBackend) RemoveConnectionNetwork(
	_ context.Context,
	name string,
) error {
	backend.removeNetworkCalls = append(backend.removeNetworkCalls, name)
	if backend.connectionErr != nil {
		return backend.connectionErr
	}
	delete(backend.connectionStatuses, name)
	return nil
}

func TestEnsureCreatesStartsAndPersistsReadyBox(t *testing.T) {
	root := t.TempDir()
	store := state.NewStore(root)
	backend := &fakeBackend{status: readyComposeStatus()}
	manager := sandbox.NewManager(
		backend,
		store,
		lock.NewManager(root),
		func() time.Time { return time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC) },
	)

	got, err := manager.Ensure(context.Background(), testSpec())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.Ready || got.ID != "a4f8c9137d2b" {
		t.Fatalf("box = %#v", got)
	}
	if len(backend.upCalls) != 1 {
		t.Fatalf("up calls = %d", len(backend.upCalls))
	}
	saved, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if saved.Boxes[got.ID].Status != state.Ready {
		t.Fatalf("saved box = %#v", saved.Boxes[got.ID])
	}
	if _, err := os.Stat(got.ComposePath); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureAndInspectAttachTransientLoopbackStatus(t *testing.T) {
	root := t.TempDir()
	store := state.NewStore(root)
	want := loopback.Status{
		EventStream: loopback.EventStreamConnected,
		Routes: []loopback.Route{{
			Port: 5173,
		}},
	}
	backend := &fakeBackend{status: readyComposeStatus(), loopback: want}
	manager := sandbox.NewManager(backend, store, lock.NewManager(root), time.Now)

	ensured, err := manager.Ensure(context.Background(), testSpec())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ensured.Loopback, want) {
		t.Fatalf("ensure loopback = %#v", ensured.Loopback)
	}
	inspected, err := manager.Inspect(context.Background(), ensured.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(inspected.Loopback, want) {
		t.Fatalf("inspect loopback = %#v", inspected.Loopback)
	}
	if len(backend.statusCalls) != 2 {
		t.Fatalf("status calls = %d; want 2", len(backend.statusCalls))
	}
}

func TestInspectReportsUnavailableLoopbackWithoutFailingBox(t *testing.T) {
	root := t.TempDir()
	store := state.NewStore(root)
	record := existingRecord(t, store, state.Ready)
	saveState(t, store, record)
	backend := &fakeBackend{
		status:      readyComposeStatus(),
		loopbackErr: errors.New("sidecar unavailable"),
	}
	manager := sandbox.NewManager(backend, store, lock.NewManager(root), time.Now)

	got, err := manager.Inspect(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.Ready ||
		got.Loopback.EventStream != loopback.EventStreamUnavailable {
		t.Fatalf("box = %#v", got)
	}
}

func TestSyncLoopbackDelegatesToSelectedProject(t *testing.T) {
	root := t.TempDir()
	store := state.NewStore(root)
	record := existingRecord(t, store, state.Ready)
	saveState(t, store, record)
	want := loopback.Status{EventStream: loopback.EventStreamConnected}
	backend := &fakeBackend{loopback: want}
	manager := sandbox.NewManager(backend, store, lock.NewManager(root), time.Now)

	got, err := manager.SyncLoopback(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) || len(backend.syncCalls) != 1 ||
		backend.syncCalls[0].Name != record.ProjectName {
		t.Fatalf("status=%#v calls=%#v", got, backend.syncCalls)
	}
}

func TestEnsureAllocatedReservesNextFreeBlockUnderManagerLock(t *testing.T) {
	root := t.TempDir()
	store := state.NewStore(root)
	existing := existingRecord(t, store, state.Ready)
	saveState(t, store, existing)
	backend := &fakeBackend{
		status: readyComposeStatus(),
		managed: []compose.ManagedProject{{
			ID:          existing.ID,
			Worktree:    existing.Worktree,
			ProjectName: existing.ProjectName,
			State:       compose.Running,
			Healthy:     true,
			Ports:       existing.Ports,
		}},
	}
	manager := sandbox.NewManager(backend, store, lock.NewManager(root), time.Now)
	spec := testSpec()
	spec.ID = "bbbbbbbbbbbb"
	spec.Worktree = "/repo/feature-billing"
	spec.Ports = ports.Block{}

	got, err := manager.EnsureAllocated(
		context.Background(),
		spec,
		ports.NewAllocator(23000, 10),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.Ports != (ports.Block{Start: 23010, Size: 10}) {
		t.Fatalf("ports = %#v", got.Ports)
	}
}

func TestEnsureIsIdempotentWhenDockerIsAlreadyReady(t *testing.T) {
	root := t.TempDir()
	store := state.NewStore(root)
	backend := &fakeBackend{status: readyComposeStatus()}
	manager := sandbox.NewManager(backend, store, lock.NewManager(root), time.Now)

	if _, err := manager.Ensure(context.Background(), testSpec()); err != nil {
		t.Fatal(err)
	}
	backend.upCalls = nil

	got, err := manager.Ensure(context.Background(), testSpec())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.Ready {
		t.Fatalf("status = %s", got.Status)
	}
	if len(backend.upCalls) != 0 || len(backend.startCalls) != 0 {
		t.Fatalf("unexpected lifecycle calls: up=%d start=%d", len(backend.upCalls), len(backend.startCalls))
	}
}

func TestEnsureReappliesReadyComposeWhenGeneratedConfigurationChanges(t *testing.T) {
	root := t.TempDir()
	store := state.NewStore(root)
	backend := &fakeBackend{status: readyComposeStatus()}
	manager := sandbox.NewManager(backend, store, lock.NewManager(root), time.Now)

	if _, err := manager.Ensure(context.Background(), testSpec()); err != nil {
		t.Fatal(err)
	}
	backend.upCalls = nil

	changed := testSpec()
	changed.Config.Runtime.WebtopImage = "wktbox/webtop:next"
	if _, err := manager.Ensure(context.Background(), changed); err != nil {
		t.Fatal(err)
	}
	if len(backend.upCalls) != 1 {
		t.Fatalf("up calls = %d, want 1", len(backend.upCalls))
	}
}

func TestEnsureReappliesReadyComposeWhenGatewayProfileChanges(t *testing.T) {
	root := t.TempDir()
	store := state.NewStore(root)
	backend := &fakeBackend{status: readyComposeStatus()}
	manager := sandbox.NewManager(backend, store, lock.NewManager(root), time.Now)

	if _, err := manager.Ensure(context.Background(), testSpec()); err != nil {
		t.Fatal(err)
	}
	backend.upCalls = nil

	changed := testSpec()
	changed.Config.Gateway.Enabled = true
	if _, err := manager.Ensure(context.Background(), changed); err != nil {
		t.Fatal(err)
	}
	if len(backend.upCalls) != 1 {
		t.Fatalf("up calls = %d, want 1", len(backend.upCalls))
	}
}

func TestInspectPrefersDockerStatusOverCachedStatus(t *testing.T) {
	root := t.TempDir()
	store := state.NewStore(root)
	record := existingRecord(t, store, state.Stopped)
	saveState(t, store, record)
	backend := &fakeBackend{status: readyComposeStatus()}
	manager := sandbox.NewManager(backend, store, lock.NewManager(root), time.Now)

	got, err := manager.Inspect(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.Ready {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestStopPreservesGeneratedFilesAndMarksStopped(t *testing.T) {
	root := t.TempDir()
	store := state.NewStore(root)
	record := existingRecord(t, store, state.Ready)
	saveState(t, store, record)
	backend := &fakeBackend{status: compose.Status{Exists: true, State: compose.Stopped}}
	manager := sandbox.NewManager(backend, store, lock.NewManager(root), time.Now)

	if err := manager.Stop(context.Background(), record.ID); err != nil {
		t.Fatal(err)
	}
	if len(backend.stopCalls) != 1 {
		t.Fatalf("stop calls = %d", len(backend.stopCalls))
	}
	if _, err := os.Stat(record.ComposePath); err != nil {
		t.Fatalf("generated file removed by stop: %v", err)
	}
	saved, _ := store.Load(context.Background())
	if saved.Boxes[record.ID].Status != state.Stopped {
		t.Fatalf("saved status = %s", saved.Boxes[record.ID].Status)
	}
}

func TestTouchUpdatesLastUsedAtWithoutChangingLifecycleState(t *testing.T) {
	root := t.TempDir()
	store := state.NewStore(root)
	record := existingRecord(t, store, state.Ready)
	record.LastUsedAt = time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	saveState(t, store, record)
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	backend := &fakeBackend{managed: []compose.ManagedProject{{
		ID:          record.ID,
		Worktree:    record.Worktree,
		ProjectName: record.ProjectName,
		State:       compose.Running,
		Healthy:     true,
		Ports:       record.Ports,
	}}}
	manager := sandbox.NewManager(backend, store, lock.NewManager(root), func() time.Time {
		return now
	})

	if err := manager.Touch(context.Background(), record.ID); err != nil {
		t.Fatal(err)
	}

	saved, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := saved.Boxes[record.ID]
	if !got.LastUsedAt.Equal(now) {
		t.Fatalf("last used = %s", got.LastUsedAt)
	}
	if got.Status != state.Ready {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestDestroyRemovesOnlySelectedBoxAndItsFiles(t *testing.T) {
	root := t.TempDir()
	store := state.NewStore(root)
	selected := existingRecord(t, store, state.Ready)
	other := selected
	other.ID = "bbbbbbbbbbbb"
	other.ProjectName = "wktbox-bbbbbbbbbbbb"
	other.Worktree = "/other"
	otherDir, err := store.BoxDir(other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(otherDir, 0o700); err != nil {
		t.Fatal(err)
	}
	other.ComposePath = filepath.Join(otherDir, "compose.yml")
	if err := os.WriteFile(other.ComposePath, []byte("other"), 0o600); err != nil {
		t.Fatal(err)
	}
	saveState(t, store, selected, other)
	backend := &fakeBackend{}
	manager := sandbox.NewManager(backend, store, lock.NewManager(root), time.Now)

	if err := manager.Destroy(context.Background(), selected.ID); err != nil {
		t.Fatal(err)
	}
	if len(backend.downCalls) != 1 || !backend.downCalls[0].volumes {
		t.Fatalf("down calls = %#v", backend.downCalls)
	}
	if _, err := os.Stat(filepath.Dir(selected.ComposePath)); !os.IsNotExist(err) {
		t.Fatalf("selected box directory still exists: %v", err)
	}
	if _, err := os.Stat(other.ComposePath); err != nil {
		t.Fatalf("other box was changed: %v", err)
	}
	saved, _ := store.Load(context.Background())
	if _, exists := saved.Boxes[selected.ID]; exists {
		t.Fatal("selected box remains in state")
	}
	if _, exists := saved.Boxes[other.ID]; !exists {
		t.Fatal("other box missing from state")
	}
}

func TestListRecoversManagedBoxAfterStateLoss(t *testing.T) {
	root := t.TempDir()
	store := state.NewStore(root)
	backend := &fakeBackend{managed: []compose.ManagedProject{{
		ID:             "a4f8c9137d2b",
		Worktree:       "/repo/feature-auth",
		ProjectName:    "wktbox-a4f8c9137d2b",
		State:          compose.Running,
		Healthy:        true,
		Ports:          ports.Block{Start: 23000, Size: 10},
		GatewayEnabled: true,
	}}}
	manager := sandbox.NewManager(backend, store, lock.NewManager(root), time.Now)

	got, err := manager.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "a4f8c9137d2b" || got[0].Status != state.Ready {
		t.Fatalf("boxes = %#v", got)
	}
	if !got[0].GatewayEnabled {
		t.Fatalf("gateway profile was not recovered: %#v", got[0])
	}
	saved, _ := store.Load(context.Background())
	if _, exists := saved.Boxes["a4f8c9137d2b"]; !exists {
		t.Fatal("recovered box was not cached")
	}
}

func TestEnsurePersistsErrorStateWhenComposeFails(t *testing.T) {
	root := t.TempDir()
	store := state.NewStore(root)
	backend := &fakeBackend{upErr: errors.New("daemon unavailable")}
	manager := sandbox.NewManager(backend, store, lock.NewManager(root), time.Now)

	_, err := manager.Ensure(context.Background(), testSpec())
	if err == nil {
		t.Fatal("expected ensure error")
	}
	saved, loadErr := store.Load(context.Background())
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if saved.Boxes["a4f8c9137d2b"].Status != state.Error {
		t.Fatalf("saved status = %s", saved.Boxes["a4f8c9137d2b"].Status)
	}
}

func readyComposeStatus() compose.Status {
	return compose.Status{
		Exists: true,
		State:  compose.Running,
		Containers: []compose.ContainerStatus{
			{Service: "docker", State: "running", Health: "healthy"},
			{Service: "webtop", State: "running", Health: "healthy"},
			{Service: "loopback", State: "running", Health: "healthy"},
			{Service: "interconnect", State: "running", Health: "healthy"},
		},
	}
}

func existingRecord(t *testing.T, store state.Store, status state.Status) state.BoxRecord {
	t.Helper()
	directory, err := store.BoxDir("a4f8c9137d2b")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	composePath := filepath.Join(directory, "compose.yml")
	envPath := filepath.Join(directory, "sandbox.env")
	if err := os.WriteFile(composePath, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envPath, []byte("WKTBOX_ID=a4f8c9137d2b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return state.BoxRecord{
		ID:             "a4f8c9137d2b",
		Name:           "feature-auth",
		Worktree:       "/repo/feature-auth",
		ProjectName:    "wktbox-a4f8c9137d2b",
		Status:         status,
		Ports:          ports.Block{Start: 23000, Size: 10},
		ComposePath:    composePath,
		SandboxEnvPath: envPath,
	}
}

func saveState(t *testing.T, store state.Store, records ...state.BoxRecord) {
	t.Helper()
	current := state.Empty()
	for _, record := range records {
		current.Boxes[record.ID] = record
	}
	if err := store.Save(context.Background(), current); err != nil {
		t.Fatal(err)
	}
}
