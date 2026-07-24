package sandbox_test

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"wktbox/internal/compose"
	"wktbox/internal/lock"
	"wktbox/internal/portforward"
	"wktbox/internal/ports"
	"wktbox/internal/sandbox"
	"wktbox/internal/state"
)

type fakeRelayLifecycle struct {
	ensureCalls []portforward.RelayProcessOptions
	stopCalls   []portforward.RelayProcessOptions
	ensureErr   error
	stopErr     error
}

func (relay *fakeRelayLifecycle) Ensure(
	_ context.Context,
	options portforward.RelayProcessOptions,
) error {
	relay.ensureCalls = append(relay.ensureCalls, options)
	return relay.ensureErr
}

func (relay *fakeRelayLifecycle) Stop(
	_ context.Context,
	options portforward.RelayProcessOptions,
) error {
	relay.stopCalls = append(relay.stopCalls, options)
	return relay.stopErr
}

type portManagerFixture struct {
	manager sandbox.Manager
	backend *fakeBackend
	relay   *fakeRelayLifecycle
	store   state.Store
	record  state.BoxRecord
}

func TestImportBatchConflictLeavesStateAndFilesUnchanged(t *testing.T) {
	fixture := newPortManagerFixture(t)
	fixture.backend.portPreflightErr = errors.New(
		`workload api already uses localhost:1234 required by import "import-1234"`,
	)
	beforeState := loadPortFixtureState(t, fixture.store)
	beforeConfig := readPortFixtureFile(t, fixture.record.PortConfigPath)

	_, err := fixture.manager.ImportPorts(
		context.Background(),
		fixture.record.ID,
		[]portforward.Mapping{testImportMapping("import-1234", 1234, 1234)},
	)
	if err == nil || !strings.Contains(err.Error(), "api already uses localhost:1234") {
		t.Fatalf("error = %v", err)
	}
	if got := loadPortFixtureState(t, fixture.store); !reflect.DeepEqual(got, beforeState) {
		t.Fatalf("state changed:\n before=%#v\n after=%#v", beforeState, got)
	}
	if got := readPortFixtureFile(t, fixture.record.PortConfigPath); string(got) != string(beforeConfig) {
		t.Fatalf("config changed:\n before=%s\n after=%s", beforeConfig, got)
	}
	if len(fixture.backend.portApplyCalls) != 0 ||
		len(fixture.backend.portRuntimeCalls) != 0 ||
		len(fixture.relay.ensureCalls) != 0 {
		t.Fatalf(
			"runtime mutated: import=%d publish=%d relay=%d",
			len(fixture.backend.portApplyCalls),
			len(fixture.backend.portRuntimeCalls),
			len(fixture.relay.ensureCalls),
		)
	}
}

func TestPublicationActivationFailureRollsBackFilesRuntimeAndState(t *testing.T) {
	fixture := newPortManagerFixture(t)
	fixture.backend.portRuntimeErrors = []error{
		errors.New("bind address already in use"),
		nil,
	}
	beforeState := loadPortFixtureState(t, fixture.store)
	beforeConfig := readPortFixtureFile(t, fixture.record.PortConfigPath)
	beforeOverride, beforeOverrideExists := readOptionalPortFixtureFile(
		t,
		portFixtureOverridePath(fixture.record),
	)
	hostPort := reservePortFixturePort(t)

	_, err := fixture.manager.PublishPorts(
		context.Background(),
		fixture.record.ID,
		[]portforward.Mapping{testPublishMapping("api", hostPort, 8000)},
	)
	if err == nil || !strings.Contains(err.Error(), "bind address already in use") {
		t.Fatalf("error = %v", err)
	}
	if got := loadPortFixtureState(t, fixture.store); !reflect.DeepEqual(got, beforeState) {
		t.Fatalf("state changed:\n before=%#v\n after=%#v", beforeState, got)
	}
	if got := readPortFixtureFile(t, fixture.record.PortConfigPath); string(got) != string(beforeConfig) {
		t.Fatalf("config changed:\n before=%s\n after=%s", beforeConfig, got)
	}
	afterOverride, afterOverrideExists := readOptionalPortFixtureFile(
		t,
		portFixtureOverridePath(fixture.record),
	)
	if afterOverrideExists != beforeOverrideExists ||
		string(afterOverride) != string(beforeOverride) {
		t.Fatalf(
			"override changed: before=(%v,%q) after=(%v,%q)",
			beforeOverrideExists,
			beforeOverride,
			afterOverrideExists,
			afterOverride,
		)
	}
	if len(fixture.backend.portRuntimeCalls) != 2 {
		t.Fatalf(
			"publication apply calls = %d, want candidate plus rollback",
			len(fixture.backend.portRuntimeCalls),
		)
	}
}

func TestImportActivationRaceRollsBackRelayFilesRuntimeAndState(t *testing.T) {
	fixture := newPortManagerFixture(t)
	fixture.backend.portApplyErrors = []error{
		errors.New("workload claimed localhost:1234 during activation"),
		nil,
	}
	beforeState := loadPortFixtureState(t, fixture.store)
	beforeConfig := readPortFixtureFile(t, fixture.record.PortConfigPath)

	_, err := fixture.manager.ImportPorts(
		context.Background(),
		fixture.record.ID,
		[]portforward.Mapping{testImportMapping("api", 1234, 1234)},
	)
	if err == nil || !strings.Contains(err.Error(), "claimed localhost:1234") {
		t.Fatalf("error = %v", err)
	}
	if got := loadPortFixtureState(t, fixture.store); !reflect.DeepEqual(got, beforeState) {
		t.Fatalf("state changed:\n before=%#v\n after=%#v", beforeState, got)
	}
	if got := readPortFixtureFile(t, fixture.record.PortConfigPath); string(got) != string(beforeConfig) {
		t.Fatalf("config changed:\n before=%s\n after=%s", beforeConfig, got)
	}
	if len(fixture.backend.portApplyCalls) != 2 ||
		len(fixture.relay.ensureCalls) != 1 ||
		len(fixture.relay.stopCalls) != 1 {
		t.Fatalf(
			"apply=%d ensure=%d stop=%d",
			len(fixture.backend.portApplyCalls),
			len(fixture.relay.ensureCalls),
			len(fixture.relay.stopCalls),
		)
	}
}

func TestPublicationHostConflictRejectsWholeBatchBeforeMutation(t *testing.T) {
	fixture := newPortManagerFixture(t)
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	port := uint16(occupied.Addr().(*net.TCPAddr).Port)
	beforeConfig := readPortFixtureFile(t, fixture.record.PortConfigPath)

	_, err = fixture.manager.PublishPorts(
		context.Background(),
		fixture.record.ID,
		[]portforward.Mapping{
			testPublishMapping("free", reservePortFixturePort(t), 8000),
			testPublishMapping("conflict", port, 9000),
		},
	)
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("error = %v", err)
	}
	if got := readPortFixtureFile(t, fixture.record.PortConfigPath); string(got) != string(beforeConfig) {
		t.Fatalf("config changed:\n before=%s\n after=%s", beforeConfig, got)
	}
	if len(fixture.backend.portRuntimeCalls) != 0 {
		t.Fatalf("runtime calls = %d", len(fixture.backend.portRuntimeCalls))
	}
}

func TestMultipleImportsActivateAsOneBatch(t *testing.T) {
	fixture := newPortManagerFixture(t)
	got, err := fixture.manager.ImportPorts(
		context.Background(),
		fixture.record.ID,
		[]portforward.Mapping{
			testImportMapping("api", 1234, 1234),
			testImportMapping("database", 5432, 15432),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 ||
		len(fixture.backend.portPreflightCalls) != 1 ||
		len(fixture.backend.portApplyCalls) != 1 ||
		len(fixture.relay.ensureCalls) != 1 {
		t.Fatalf(
			"mappings=%#v preflight=%d apply=%d relay=%d",
			got,
			len(fixture.backend.portPreflightCalls),
			len(fixture.backend.portApplyCalls),
			len(fixture.relay.ensureCalls),
		)
	}
	if fixture.backend.portApplyCalls[0].PortMappingsEnabled {
		t.Fatal("import-only batch unexpectedly enabled portbridge")
	}
	saved := loadPortFixtureState(t, fixture.store)
	if len(saved.Boxes[fixture.record.ID].PortMappings) != 2 {
		t.Fatalf("saved mappings = %#v", saved.Boxes[fixture.record.ID].PortMappings)
	}
}

func TestRemoveLastImportClosesProxiesAndHostRelay(t *testing.T) {
	fixture := newPortManagerFixture(t)
	if _, err := fixture.manager.ImportPorts(
		context.Background(),
		fixture.record.ID,
		[]portforward.Mapping{
			testImportMapping("api", 1234, 1234),
			testImportMapping("database", 5432, 15432),
		},
	); err != nil {
		t.Fatal(err)
	}
	fixture.relay.stopCalls = nil
	fixture.backend.portApplyCalls = nil

	got, err := fixture.manager.RemovePortMappings(
		context.Background(),
		fixture.record.ID,
		nil,
		portforward.Import,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 ||
		len(fixture.relay.stopCalls) != 1 ||
		len(fixture.backend.portApplyCalls) != 1 {
		t.Fatalf(
			"mappings=%#v relay stops=%d import apply=%d",
			got,
			len(fixture.relay.stopCalls),
			len(fixture.backend.portApplyCalls),
		)
	}
}

func TestDuplicateMappingNameRejectsBatchBeforePreflight(t *testing.T) {
	fixture := newPortManagerFixture(t)
	if _, err := fixture.manager.ImportPorts(
		context.Background(),
		fixture.record.ID,
		[]portforward.Mapping{testImportMapping("api", 1234, 1234)},
	); err != nil {
		t.Fatal(err)
	}
	fixture.backend.portPreflightCalls = nil

	_, err := fixture.manager.PublishPorts(
		context.Background(),
		fixture.record.ID,
		[]portforward.Mapping{
			testPublishMapping("api", reservePortFixturePort(t), 8000),
		},
	)
	if err == nil || !strings.Contains(err.Error(), `duplicate mapping name "api"`) {
		t.Fatalf("error = %v", err)
	}
	if len(fixture.backend.portPreflightCalls) != 0 ||
		len(fixture.backend.portRuntimeCalls) != 0 {
		t.Fatalf(
			"runtime touched: preflight=%d runtime=%d",
			len(fixture.backend.portPreflightCalls),
			len(fixture.backend.portRuntimeCalls),
		)
	}
}

func TestStopPreservesImportsAndStopsHostRelay(t *testing.T) {
	fixture := newPortManagerFixture(t)
	if _, err := fixture.manager.ImportPorts(
		context.Background(),
		fixture.record.ID,
		[]portforward.Mapping{testImportMapping("api", 1234, 1234)},
	); err != nil {
		t.Fatal(err)
	}
	fixture.relay.ensureCalls = nil
	fixture.relay.stopCalls = nil
	fixture.backend.portApplyCalls = nil

	if err := fixture.manager.Stop(
		context.Background(),
		fixture.record.ID,
	); err != nil {
		t.Fatal(err)
	}
	if len(fixture.relay.stopCalls) != 1 {
		t.Fatalf("relay stop calls = %d", len(fixture.relay.stopCalls))
	}
	saved := loadPortFixtureState(t, fixture.store).Boxes[fixture.record.ID]
	if saved.Status != state.Stopped || len(saved.PortMappings) != 1 {
		t.Fatalf("saved box = %#v", saved)
	}
}

func TestRestartRestoresImportRelayAndWorkloadProxies(t *testing.T) {
	fixture := newPortManagerFixture(t)
	if _, err := fixture.manager.ImportPorts(
		context.Background(),
		fixture.record.ID,
		[]portforward.Mapping{testImportMapping("api", 1234, 1234)},
	); err != nil {
		t.Fatal(err)
	}
	fixture.relay.ensureCalls = nil
	fixture.backend.portApplyCalls = nil

	if err := fixture.manager.Restart(
		context.Background(),
		fixture.record.ID,
	); err != nil {
		t.Fatal(err)
	}
	if len(fixture.relay.ensureCalls) != 1 ||
		len(fixture.backend.portApplyCalls) != 1 {
		t.Fatalf(
			"relay ensure=%d import apply=%d",
			len(fixture.relay.ensureCalls),
			len(fixture.backend.portApplyCalls),
		)
	}
}

func newPortManagerFixture(t *testing.T) portManagerFixture {
	t.Helper()
	root := t.TempDir()
	store := state.NewStore(root)
	record := existingRecord(t, store, state.Ready)
	directory := filepath.Dir(record.ComposePath)
	record.PortConfigPath = filepath.Join(directory, "ports.json")
	record.PortRelayTokenPath = filepath.Join(directory, "port-relay.token")
	config, err := portforward.RenderConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(record.PortConfigPath, config, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		record.PortRelayTokenPath,
		make([]byte, portforward.TokenSize),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	saveState(t, store, record)
	backend := &fakeBackend{
		status: readyComposeStatus(),
		managed: []compose.ManagedProject{{
			ID:          record.ID,
			Worktree:    record.Worktree,
			ProjectName: record.ProjectName,
			State:       compose.Running,
			Healthy:     true,
			Ports:       ports.Block{Start: 23000, Size: 10},
		}},
	}
	relay := &fakeRelayLifecycle{}
	manager := sandbox.NewManagerWithOptions(
		backend,
		store,
		lock.NewManager(root),
		func() time.Time { return time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC) },
		sandbox.ManagerOptions{Relay: relay},
	)
	return portManagerFixture{
		manager: manager,
		backend: backend,
		relay:   relay,
		store:   store,
		record:  record,
	}
}

func portFixtureOverridePath(record state.BoxRecord) string {
	return filepath.Join(filepath.Dir(record.PortConfigPath), "ports.override.yml")
}

func testImportMapping(name string, source uint16, target uint16) portforward.Mapping {
	return portforward.Mapping{
		Name:          name,
		Direction:     portforward.Import,
		SourceAddress: "127.0.0.1",
		SourcePort:    source,
		TargetPort:    target,
	}
}

func testPublishMapping(name string, source uint16, target uint16) portforward.Mapping {
	return portforward.Mapping{
		Name:          name,
		Direction:     portforward.Publish,
		SourceAddress: "127.0.0.1",
		SourcePort:    source,
		TargetPort:    target,
	}
}

func reservePortFixturePort(t *testing.T) uint16 {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func loadPortFixtureState(t *testing.T, store state.Store) state.State {
	t.Helper()
	current, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return current
}

func readPortFixtureFile(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func readOptionalPortFixtureFile(t *testing.T, path string) ([]byte, bool) {
	t.Helper()
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return body, true
}
