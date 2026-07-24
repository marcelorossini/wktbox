package sandbox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"time"

	"wktbox/internal/compose"
	"wktbox/internal/lock"
	"wktbox/internal/loopback"
	"wktbox/internal/portforward"
	"wktbox/internal/state"
)

func (manager Manager) ImportPorts(
	ctx context.Context,
	id string,
	mappings []portforward.Mapping,
) ([]portforward.ObservedMapping, error) {
	for _, mapping := range mappings {
		if mapping.Direction != portforward.Import {
			return nil, fmt.Errorf(
				"mapping %q must use direction %q",
				mapping.Name,
				portforward.Import,
			)
		}
	}
	return manager.addPortMappings(ctx, id, mappings, true)
}

func (manager Manager) PublishPorts(
	ctx context.Context,
	id string,
	mappings []portforward.Mapping,
) ([]portforward.ObservedMapping, error) {
	for _, mapping := range mappings {
		if mapping.Direction != portforward.Publish {
			return nil, fmt.Errorf(
				"mapping %q must use direction %q",
				mapping.Name,
				portforward.Publish,
			)
		}
	}
	return manager.addPortMappings(ctx, id, mappings, false)
}

func (manager Manager) PortMappings(
	ctx context.Context,
	id string,
) ([]portforward.ObservedMapping, error) {
	unlock, err := manager.lockBox(ctx, id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	current, err := manager.loadAndReconcile(ctx)
	if err != nil {
		return nil, err
	}
	record, exists := current.Boxes[id]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrBoxNotFound, id)
	}
	return manager.observePortMappings(ctx, record), nil
}

func (manager Manager) RemovePortMappings(
	ctx context.Context,
	id string,
	names []string,
	direction portforward.Direction,
) ([]portforward.ObservedMapping, error) {
	unlock, err := manager.lockBox(ctx, id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	current, record, err := manager.portRecord(ctx, id)
	if err != nil {
		return nil, err
	}
	if record.Status != state.Ready && record.Status != state.Stopped {
		return nil, fmt.Errorf(
			"box %s is %s; port mappings can only be removed while ready or stopped",
			id,
			record.Status,
		)
	}
	removeNames := make(map[string]bool, len(names))
	for _, name := range names {
		removeNames[name] = true
	}
	if len(removeNames) == 0 &&
		direction != portforward.Import &&
		direction != portforward.Publish {
		return nil, errors.New("mapping names or a direction are required")
	}
	candidateMappings := make([]portforward.Mapping, 0, len(record.PortMappings))
	removed := make(map[string]bool)
	for _, mapping := range record.PortMappings {
		remove := removeNames[mapping.Name]
		if len(removeNames) == 0 {
			remove = mapping.Direction == direction
		}
		if remove {
			removed[mapping.Name] = true
			continue
		}
		candidateMappings = append(candidateMappings, mapping)
	}
	for name := range removeNames {
		if !removed[name] {
			return nil, fmt.Errorf("port mapping %q does not exist", name)
		}
	}
	if len(removeNames) == 0 && len(removed) == 0 {
		return nil, fmt.Errorf("no %s port mappings exist", direction)
	}
	return manager.applyPortMappings(ctx, current, record, candidateMappings)
}

func (manager Manager) addPortMappings(
	ctx context.Context,
	id string,
	mappings []portforward.Mapping,
	preflightImports bool,
) ([]portforward.ObservedMapping, error) {
	if len(mappings) == 0 {
		return nil, errors.New("at least one port mapping is required")
	}
	unlock, err := manager.lockBox(ctx, id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	current, record, err := manager.readyPortRecord(ctx, id)
	if err != nil {
		return nil, err
	}
	candidateMappings := append(
		[]portforward.Mapping(nil),
		record.PortMappings...,
	)
	for _, mapping := range mappings {
		if mapping.CreatedAt.IsZero() {
			mapping.CreatedAt = manager.now().UTC()
		}
		candidateMappings = append(candidateMappings, mapping)
	}
	portforward.Sort(candidateMappings)
	if err := portforward.ValidateSet(candidateMappings); err != nil {
		return nil, err
	}
	record = manager.ensurePortRecordPaths(record)
	if preflightImports {
		if err := manager.backend.PortImportPreflight(
			ctx,
			projectFor(record),
			candidateMappings,
		); err != nil {
			return nil, fmt.Errorf("preflight port imports: %w", err)
		}
	}
	if err := preflightPublicationBindings(
		ctx,
		record.PortMappings,
		candidateMappings,
	); err != nil {
		return nil, err
	}
	return manager.applyPortMappings(ctx, current, record, candidateMappings)
}

func (manager Manager) readyPortRecord(
	ctx context.Context,
	id string,
) (state.State, state.BoxRecord, error) {
	current, record, err := manager.portRecord(ctx, id)
	if err != nil {
		return state.State{}, state.BoxRecord{}, err
	}
	if record.Status != state.Ready {
		return state.State{}, state.BoxRecord{}, fmt.Errorf(
			"box %s is %s; run wktbox up before changing port mappings",
			id,
			record.Status,
		)
	}
	return current, record, nil
}

func (manager Manager) portRecord(
	ctx context.Context,
	id string,
) (state.State, state.BoxRecord, error) {
	current, err := manager.loadAndReconcile(ctx)
	if err != nil {
		return state.State{}, state.BoxRecord{}, err
	}
	record, exists := current.Boxes[id]
	if !exists {
		return state.State{}, state.BoxRecord{}, fmt.Errorf(
			"%w: %s",
			ErrBoxNotFound,
			id,
		)
	}
	return current, manager.ensurePortRecordPaths(record), nil
}

func (manager Manager) ensurePortRecordPaths(
	record state.BoxRecord,
) state.BoxRecord {
	directory := filepath.Dir(record.ComposePath)
	if record.PortConfigPath == "" {
		record.PortConfigPath = filepath.Join(directory, "ports", "ports.json")
	}
	if record.PortRelayTokenPath == "" {
		record.PortRelayTokenPath = filepath.Join(directory, "ports", "relay.token")
	}
	return record
}

func (manager Manager) applyPortMappings(
	ctx context.Context,
	current state.State,
	previous state.BoxRecord,
	mappings []portforward.Mapping,
) ([]portforward.ObservedMapping, error) {
	transactionLockPath := filepath.Join(
		filepath.Dir(previous.ComposePath),
		"port-transaction",
		"transaction.lock",
	)
	unlock, err := lock.Acquire(ctx, transactionLockPath)
	if err != nil {
		return nil, fmt.Errorf("begin port mapping transaction: %w", err)
	}
	observed, applyErr := manager.applyPortMappingsLocked(
		ctx,
		current,
		previous,
		mappings,
	)
	return observed, errors.Join(applyErr, unlock())
}

func (manager Manager) applyPortMappingsLocked(
	ctx context.Context,
	current state.State,
	previous state.BoxRecord,
	mappings []portforward.Mapping,
) ([]portforward.ObservedMapping, error) {
	candidate := previous
	candidate.PortMappings = append([]portforward.Mapping(nil), mappings...)
	portforward.Sort(candidate.PortMappings)
	candidate.LastUsedAt = manager.now().UTC()
	overridePath := filepath.Join(
		filepath.Dir(candidate.ComposePath),
		"ports.override.yml",
	)
	if err := os.MkdirAll(filepath.Dir(candidate.PortConfigPath), 0o700); err != nil {
		return nil, fmt.Errorf("create port runtime directory: %w", err)
	}
	configSnapshot, err := capturePortFile(candidate.PortConfigPath)
	if err != nil {
		return nil, err
	}
	overrideSnapshot, err := capturePortFile(overridePath)
	if err != nil {
		return nil, err
	}
	publicationsChanged := !reflect.DeepEqual(
		portforward.Filter(previous.PortMappings, portforward.Publish),
		portforward.Filter(candidate.PortMappings, portforward.Publish),
	)
	importsChanged := !reflect.DeepEqual(
		portforward.Filter(previous.PortMappings, portforward.Import),
		portforward.Filter(candidate.PortMappings, portforward.Import),
	)
	overrideExists, err := RenderPortFiles(
		candidate.PortConfigPath,
		overridePath,
		candidate.PortMappings,
	)
	if err != nil {
		return nil, errors.Join(
			err,
			restorePortFile(configSnapshot),
			restorePortFile(overrideSnapshot),
		)
	}
	if overrideExists {
		candidate.PortOverridePath = overridePath
	} else {
		candidate.PortOverridePath = ""
	}
	if previous.Status == state.Stopped {
		current.Boxes[candidate.ID] = candidate
		saveErr := manager.store.Save(ctx, current)
		if saveErr == nil {
			return observedPortMappings(candidate), nil
		}
		restoreErr := errors.Join(
			restorePortFile(configSnapshot),
			restorePortFile(overrideSnapshot),
		)
		if restoreErr != nil {
			previous.Status = state.Error
			previous.PortRuntimeError = restoreErr.Error()
		}
		current.Boxes[previous.ID] = previous
		rollbackErr := manager.store.Save(ctx, current)
		return nil, errors.Join(
			fmt.Errorf("save stopped box port mappings: %w", saveErr),
			restoreErr,
			rollbackErr,
		)
	}

	activationErr := manager.activatePortMappings(
		ctx,
		previous,
		candidate,
		publicationsChanged,
		importsChanged,
	)
	if activationErr == nil {
		current.Boxes[candidate.ID] = candidate
		activationErr = manager.store.Save(ctx, current)
	}
	if activationErr != nil {
		current.Boxes[previous.ID] = previous
		rollbackCtx, cancel := context.WithTimeout(
			context.Background(),
			15*time.Second,
		)
		defer cancel()
		rollbackErr := manager.rollbackPortMappings(
			rollbackCtx,
			current,
			previous,
			configSnapshot,
			overrideSnapshot,
			publicationsChanged,
			importsChanged,
		)
		return nil, errors.Join(activationErr, rollbackErr)
	}
	return observedPortMappings(candidate), nil
}

func (manager Manager) activatePortMappings(
	ctx context.Context,
	previous state.BoxRecord,
	candidate state.BoxRecord,
	publicationsChanged bool,
	importsChanged bool,
) error {
	candidateImports := portforward.Filter(
		candidate.PortMappings,
		portforward.Import,
	)
	if len(candidateImports) != 0 {
		if err := manager.relay.Ensure(ctx, relayOptions(candidate)); err != nil {
			return fmt.Errorf("start host import relay: %w", err)
		}
	}
	if publicationsChanged {
		if err := manager.backend.PortRuntimeApply(
			ctx,
			projectFor(candidate),
		); err != nil {
			return fmt.Errorf("apply port publications: %w", err)
		}
	}
	if importsChanged {
		if _, err := manager.backend.PortImportApply(
			ctx,
			projectFor(candidate),
		); err != nil {
			return fmt.Errorf("apply port imports: %w", err)
		}
	}
	if len(candidateImports) == 0 &&
		len(portforward.Filter(previous.PortMappings, portforward.Import)) != 0 {
		if err := manager.relay.Stop(ctx, relayOptions(candidate)); err != nil {
			return fmt.Errorf("stop host import relay: %w", err)
		}
	}
	return nil
}

func (manager Manager) rollbackPortMappings(
	ctx context.Context,
	current state.State,
	previous state.BoxRecord,
	configSnapshot portFileSnapshot,
	overrideSnapshot portFileSnapshot,
	publicationsChanged bool,
	importsChanged bool,
) error {
	var result error
	result = errors.Join(result, restorePortFile(configSnapshot))
	result = errors.Join(result, restorePortFile(overrideSnapshot))
	previousImports := portforward.Filter(
		previous.PortMappings,
		portforward.Import,
	)
	if len(previousImports) != 0 {
		result = errors.Join(
			result,
			manager.relay.Ensure(ctx, relayOptions(previous)),
		)
	}
	if publicationsChanged {
		result = errors.Join(
			result,
			manager.backend.PortRuntimeApply(ctx, projectFor(previous)),
		)
	}
	if importsChanged {
		_, err := manager.backend.PortImportApply(ctx, projectFor(previous))
		result = errors.Join(result, err)
	}
	if len(previousImports) == 0 {
		result = errors.Join(
			result,
			manager.relay.Stop(ctx, relayOptions(previous)),
		)
	}
	if result != nil {
		previous.Status = state.Error
		previous.PortRuntimeError = result.Error()
	}
	current.Boxes[previous.ID] = previous
	saveErr := manager.store.Save(ctx, current)
	if saveErr != nil {
		previous.Status = state.Error
		persistErr := fmt.Errorf("persist rollback state: %w", saveErr)
		previous.PortRuntimeError = errors.Join(result, persistErr).Error()
		current.Boxes[previous.ID] = previous
		saveErr = errors.Join(saveErr, manager.store.Save(ctx, current))
	}
	result = errors.Join(result, saveErr)
	if result != nil {
		return fmt.Errorf("rollback port mappings: %w", result)
	}
	return nil
}

func observedPortMappings(
	record state.BoxRecord,
) []portforward.ObservedMapping {
	mappings := append([]portforward.Mapping(nil), record.PortMappings...)
	portforward.Sort(mappings)
	observedState := portforward.StateStopped
	observedError := ""
	if record.Status == state.Ready {
		observedState = portforward.StateReady
	} else if record.Status == state.Error {
		observedState = portforward.StateDegraded
		observedError = record.PortRuntimeError
	}
	result := make([]portforward.ObservedMapping, 0, len(mappings))
	for _, mapping := range mappings {
		result = append(result, portforward.ObservedMapping{
			Mapping: mapping,
			State:   observedState,
			Error:   observedError,
		})
	}
	return result
}

func (manager Manager) observePortMappings(
	ctx context.Context,
	record state.BoxRecord,
) []portforward.ObservedMapping {
	observed := observedPortMappings(record)
	if len(observed) == 0 || record.Status == state.Stopped {
		return observed
	}
	project := projectFor(record)
	runtimeStatus, err := manager.backend.Inspect(ctx, project)
	if err != nil {
		return degradePortMappings(observed, "", err.Error())
	}
	if !runtimeStatus.Exists || runtimeStatus.State == compose.Stopped {
		return setPortMappingState(
			observed,
			"",
			portforward.StateStopped,
			"",
		)
	}
	if !runtimeStatus.Ready() {
		return degradePortMappings(
			observed,
			"",
			"box runtime dependencies are not ready",
		)
	}
	if record.PortRuntimeError == "" {
		observed = setPortMappingState(
			observed,
			"",
			portforward.StateReady,
			"",
		)
	} else {
		observed = degradePortMappings(
			observed,
			"",
			record.PortRuntimeError,
		)
	}
	if !runtimeStatus.ReadyFor(project) {
		observed = degradePortMappings(
			observed,
			portforward.Publish,
			"publication portbridge is not running",
		)
	}

	imports := portforward.Filter(record.PortMappings, portforward.Import)
	if len(imports) == 0 {
		return observed
	}
	if err := manager.relay.Probe(ctx, relayOptions(record)); err != nil {
		observed = degradePortMappings(
			observed,
			portforward.Import,
			fmt.Sprintf("host import relay is unavailable: %v", err),
		)
	}
	loopbackStatus, err := manager.backend.LoopbackStatus(ctx, project)
	if err != nil {
		return degradePortMappings(
			observed,
			portforward.Import,
			fmt.Sprintf("workload import status is unavailable: %v", err),
		)
	}
	if loopbackStatus.EventStream != loopback.EventStreamConnected {
		observed = degradePortMappings(
			observed,
			portforward.Import,
			fmt.Sprintf(
				"workload import reconciler is %s",
				loopbackStatus.EventStream,
			),
		)
	}
	for _, importStatus := range loopbackStatus.Imports {
		if importStatus.State == loopback.ImportListening {
			continue
		}
		detail := importStatus.Error
		if detail == "" {
			detail = fmt.Sprintf(
				"import proxy for workload %s is %s",
				importStatus.Workload,
				importStatus.State,
			)
		}
		observed = degradeNamedPortMapping(
			observed,
			importStatus.Mapping,
			detail,
		)
	}
	for _, warning := range loopbackStatus.Warnings {
		if warning.Mapping == "" {
			continue
		}
		observed = degradeNamedPortMapping(
			observed,
			warning.Mapping,
			warning.Message,
		)
	}
	return observed
}

func degradePortMappings(
	observed []portforward.ObservedMapping,
	direction portforward.Direction,
	detail string,
) []portforward.ObservedMapping {
	return setPortMappingState(
		observed,
		direction,
		portforward.StateDegraded,
		detail,
	)
}

func setPortMappingState(
	observed []portforward.ObservedMapping,
	direction portforward.Direction,
	mappingState string,
	detail string,
) []portforward.ObservedMapping {
	for index := range observed {
		if direction != "" && observed[index].Direction != direction {
			continue
		}
		observed[index].State = mappingState
		observed[index].Error = detail
	}
	return observed
}

func degradeNamedPortMapping(
	observed []portforward.ObservedMapping,
	name string,
	detail string,
) []portforward.ObservedMapping {
	for index := range observed {
		if observed[index].Name != name {
			continue
		}
		observed[index].State = portforward.StateDegraded
		observed[index].Error = detail
	}
	return observed
}

func preflightPublicationBindings(
	ctx context.Context,
	previous []portforward.Mapping,
	candidate []portforward.Mapping,
) error {
	existing := make(map[string]bool)
	for _, mapping := range portforward.Filter(previous, portforward.Publish) {
		existing[publicationListener(mapping)] = true
	}
	listeners := make([]net.Listener, 0)
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	for _, mapping := range portforward.Filter(candidate, portforward.Publish) {
		address := publicationListener(mapping)
		if existing[address] {
			continue
		}
		listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address)
		if err != nil {
			return fmt.Errorf(
				"publication %q cannot bind %s: %w",
				mapping.Name,
				address,
				err,
			)
		}
		listeners = append(listeners, listener)
	}
	return nil
}

func publicationListener(mapping portforward.Mapping) string {
	return net.JoinHostPort(
		mapping.SourceAddress,
		strconv.Itoa(int(mapping.SourcePort)),
	)
}

func relayOptions(record state.BoxRecord) portforward.RelayProcessOptions {
	options := portforward.RelayProcessPaths(record.PortConfigPath)
	if record.PortRelayTokenPath != "" {
		options.TokenPath = record.PortRelayTokenPath
	}
	port := strconv.Itoa(record.Ports.ImportRelay())
	options.ListenAddress = net.JoinHostPort("0.0.0.0", port)
	options.ProbeAddress = net.JoinHostPort("127.0.0.1", port)
	return options
}

func (manager Manager) startImportRelay(
	ctx context.Context,
	record state.BoxRecord,
) error {
	if len(portforward.Filter(record.PortMappings, portforward.Import)) == 0 {
		return nil
	}
	if err := manager.relay.Ensure(ctx, relayOptions(record)); err != nil {
		return fmt.Errorf("start host import relay: %w", err)
	}
	return nil
}

func (manager Manager) applyActivePortRuntime(
	ctx context.Context,
	record state.BoxRecord,
) error {
	if len(portforward.Filter(record.PortMappings, portforward.Publish)) != 0 {
		if err := manager.backend.PortRuntimeApply(
			ctx,
			projectFor(record),
		); err != nil {
			return fmt.Errorf("restore port publications: %w", err)
		}
	}
	if len(portforward.Filter(record.PortMappings, portforward.Import)) != 0 {
		if _, err := manager.backend.PortImportApply(
			ctx,
			projectFor(record),
		); err != nil {
			return fmt.Errorf("restore port imports: %w", err)
		}
	}
	return nil
}

type portFileSnapshot struct {
	path   string
	body   []byte
	exists bool
}

func capturePortFile(path string) (portFileSnapshot, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return portFileSnapshot{path: path}, nil
	}
	if err != nil {
		return portFileSnapshot{}, fmt.Errorf("capture port file %s: %w", path, err)
	}
	return portFileSnapshot{
		path:   path,
		body:   body,
		exists: true,
	}, nil
}

func restorePortFile(snapshot portFileSnapshot) error {
	if !snapshot.exists {
		if err := os.Remove(snapshot.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove rolled back port file %s: %w", snapshot.path, err)
		}
		return nil
	}
	_, err := writeProtected(snapshot.path, snapshot.body)
	return err
}
