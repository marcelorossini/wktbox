package sandbox

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"wktbox/internal/compose"
	"wktbox/internal/connection"
	"wktbox/internal/state"
)

var (
	ErrConnectionNotFound = errors.New("connection not found")
	ErrSelectorNotFound   = errors.New("selector not found")
	ErrSelectorAmbiguous  = errors.New("selector is ambiguous")
)

var connectionNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

func (manager Manager) Connect(
	ctx context.Context,
	selectors []string,
	name string,
	version string,
) (state.ConnectionRecord, error) {
	unlock, err := manager.locks.Global(ctx)
	if err != nil {
		return state.ConnectionRecord{}, err
	}
	defer unlock()

	if name != "" && !connectionNamePattern.MatchString(name) {
		return state.ConnectionRecord{}, fmt.Errorf(
			"invalid connection name %q; use lowercase letters, digits, dot, underscore, or dash",
			name,
		)
	}
	current, err := manager.loadAndReconcileUnlocked(ctx)
	if err != nil {
		return state.ConnectionRecord{}, err
	}
	memberIDs := make([]string, 0, len(selectors))
	seen := make(map[string]bool, len(selectors))
	for _, selector := range selectors {
		box, err := resolveBoxSelector(current.Boxes, selector)
		if err != nil {
			return state.ConnectionRecord{}, err
		}
		if seen[box.ID] {
			return state.ConnectionRecord{}, fmt.Errorf(
				"connection member %s was selected more than once",
				box.ID,
			)
		}
		if box.Status != state.Ready {
			return state.ConnectionRecord{}, fmt.Errorf(
				"box %s is %s, not ready",
				box.ID,
				box.Status,
			)
		}
		seen[box.ID] = true
		memberIDs = append(memberIDs, box.ID)
	}
	id, err := connection.ID(memberIDs)
	if err != nil {
		return state.ConnectionRecord{}, err
	}
	sort.Strings(memberIDs)
	for existingID, existing := range current.Connections {
		if existing.Name == name && name != "" && existingID != id {
			return state.ConnectionRecord{}, fmt.Errorf(
				"connection name %q is already used by %s",
				name,
				existingID,
			)
		}
	}

	record, exists := current.Connections[id]
	if exists {
		if record.Name != "" && name != "" && record.Name != name {
			return state.ConnectionRecord{}, fmt.Errorf(
				"connection %s is already named %q",
				id,
				record.Name,
			)
		}
		if record.Name == "" && name != "" {
			record.Name = name
		}
		if version != "" {
			record.Version = version
		}
	} else {
		record = state.ConnectionRecord{
			ID:        id,
			Name:      name,
			Network:   connectionNetworkName(id),
			Version:   version,
			Members:   memberIDs,
			Status:    state.ConnectionDegraded,
			CreatedAt: manager.now().UTC(),
		}
	}
	current.Connections[id] = record
	if err := manager.store.Save(ctx, current); err != nil {
		return state.ConnectionRecord{}, err
	}

	record, reconcileErr := manager.reconcileConnection(
		ctx,
		&current,
		record,
	)
	if saveErr := manager.store.Save(ctx, current); saveErr != nil {
		if reconcileErr != nil {
			return record, errors.Join(reconcileErr, saveErr)
		}
		return record, saveErr
	}
	return record, reconcileErr
}

func (manager Manager) Connections(
	ctx context.Context,
	selector string,
) ([]state.ConnectionRecord, error) {
	unlock, err := manager.locks.Global(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()

	current, err := manager.loadAndReconcileUnlocked(ctx)
	if err != nil {
		return nil, err
	}
	records := make([]state.ConnectionRecord, 0, len(current.Connections))
	if selector != "" {
		record, err := resolveConnectionSelector(current.Connections, selector)
		if err != nil {
			return nil, err
		}
		return []state.ConnectionRecord{
			manager.observeConnection(ctx, current, record),
		}, nil
	}
	for _, record := range current.Connections {
		records = append(
			records,
			manager.observeConnection(ctx, current, record),
		)
	}
	sort.Slice(records, func(left int, right int) bool {
		return records[left].ID < records[right].ID
	})
	return records, nil
}

func (manager Manager) Disconnect(
	ctx context.Context,
	selector string,
) (state.ConnectionRecord, error) {
	unlock, err := manager.locks.Global(ctx)
	if err != nil {
		return state.ConnectionRecord{}, err
	}
	defer unlock()

	current, err := manager.loadAndReconcileUnlocked(ctx)
	if err != nil {
		return state.ConnectionRecord{}, err
	}
	record, err := resolveConnectionSelector(current.Connections, selector)
	if err != nil {
		return state.ConnectionRecord{}, err
	}
	if err := manager.disconnectConnectionRuntime(ctx, record); err != nil {
		record.Status = state.ConnectionError
		record.Error = err.Error()
		record.ReconciledAt = manager.now().UTC()
		current.Connections[record.ID] = record
		if saveErr := manager.store.Save(ctx, current); saveErr != nil {
			return record, errors.Join(err, saveErr)
		}
		return record, err
	}
	delete(current.Connections, record.ID)
	if err := manager.store.Save(ctx, current); err != nil {
		return record, err
	}
	return record, nil
}

func (manager Manager) reconcileConnection(
	ctx context.Context,
	current *state.State,
	record state.ConnectionRecord,
) (state.ConnectionRecord, error) {
	network := compose.ConnectionNetwork{
		ID:         record.ID,
		Name:       record.Name,
		DockerName: record.Network,
		Version:    record.Version,
	}
	if err := manager.backend.EnsureConnectionNetwork(ctx, network); err != nil {
		return manager.connectionFailure(current, record, err)
	}

	degraded := false
	for _, memberID := range record.Members {
		box, exists := current.Boxes[memberID]
		if !exists || box.Status != state.Ready {
			degraded = true
			continue
		}
		for _, endpoint := range connectionEndpoints(record.Network, memberID) {
			if err := manager.backend.ConnectConnectionEndpoint(ctx, endpoint); err != nil {
				return manager.connectionFailure(current, record, err)
			}
		}
	}
	status, err := manager.backend.InspectConnectionNetwork(ctx, record.Network)
	if err != nil {
		return manager.connectionFailure(current, record, err)
	}
	if !status.Exists || !status.Managed || status.ConnectionID != record.ID {
		return manager.connectionFailure(
			current,
			record,
			fmt.Errorf("connection network %s failed verification", record.Network),
		)
	}
	for _, memberID := range record.Members {
		box, exists := current.Boxes[memberID]
		if !exists || box.Status != state.Ready {
			degraded = true
			continue
		}
		for _, service := range []string{"docker", "webtop"} {
			if status.Endpoints[memberID+"/"+service] == "" {
				degraded = true
			}
		}
	}
	record.Status = state.ConnectionReady
	if degraded {
		record.Status = state.ConnectionDegraded
	}
	record.Error = ""
	record.ReconciledAt = manager.now().UTC()
	current.Connections[record.ID] = record
	return record, nil
}

func (manager Manager) observeConnection(
	ctx context.Context,
	current state.State,
	record state.ConnectionRecord,
) state.ConnectionRecord {
	status, err := manager.backend.InspectConnectionNetwork(ctx, record.Network)
	if err != nil {
		record.Status = state.ConnectionError
		record.Error = err.Error()
		return record
	}
	record.Status = state.ConnectionReady
	record.Error = ""
	if !status.Exists || !status.Managed || status.ConnectionID != record.ID {
		record.Status = state.ConnectionDegraded
		return record
	}
	for _, memberID := range record.Members {
		box, exists := current.Boxes[memberID]
		if !exists || box.Status != state.Ready {
			record.Status = state.ConnectionDegraded
			continue
		}
		for _, service := range []string{"docker", "webtop"} {
			if status.Endpoints[memberID+"/"+service] == "" {
				record.Status = state.ConnectionDegraded
			}
		}
	}
	return record
}

func (manager Manager) connectionFailure(
	current *state.State,
	record state.ConnectionRecord,
	cause error,
) (state.ConnectionRecord, error) {
	record.Status = state.ConnectionError
	record.Error = cause.Error()
	record.ReconciledAt = manager.now().UTC()
	current.Connections[record.ID] = record
	return record, cause
}

func (manager Manager) reconcileConnectionsForBox(
	ctx context.Context,
	current *state.State,
	boxID string,
) error {
	ids := sortedConnectionIDs(current.Connections)
	for _, id := range ids {
		record := current.Connections[id]
		if !containsMember(record.Members, boxID) {
			continue
		}
		if _, err := manager.reconcileConnection(
			ctx,
			current,
			record,
		); err != nil {
			return err
		}
	}
	return nil
}

func (manager Manager) removeBoxFromConnections(
	ctx context.Context,
	current *state.State,
	boxID string,
) error {
	ids := sortedConnectionIDs(current.Connections)
	for _, id := range ids {
		record := current.Connections[id]
		if !containsMember(record.Members, boxID) {
			continue
		}
		members := make([]string, 0, len(record.Members)-1)
		for _, memberID := range record.Members {
			if memberID != boxID {
				members = append(members, memberID)
			}
		}
		if len(members) < 2 {
			if err := manager.disconnectConnectionRuntime(ctx, record); err != nil {
				return err
			}
			delete(current.Connections, id)
			continue
		}
		record.Members = members
		current.Connections[id] = record
		if _, err := manager.reconcileConnection(
			ctx,
			current,
			record,
		); err != nil {
			return err
		}
	}
	return nil
}

func (manager Manager) disconnectConnectionRuntime(
	ctx context.Context,
	record state.ConnectionRecord,
) error {
	for _, memberID := range record.Members {
		for _, endpoint := range connectionEndpoints(record.Network, memberID) {
			if err := manager.backend.DisconnectConnectionEndpoint(ctx, endpoint); err != nil {
				return err
			}
		}
	}
	return manager.backend.RemoveConnectionNetwork(ctx, record.Network)
}

func connectionEndpoints(network string, boxID string) []compose.ConnectionEndpoint {
	return []compose.ConnectionEndpoint{
		{
			Network: network,
			BoxID:   boxID,
			Service: "docker",
			Alias:   connection.Alias(boxID),
		},
		{
			Network: network,
			BoxID:   boxID,
			Service: "webtop",
		},
	}
}

func resolveBoxSelector(
	boxes map[string]state.BoxRecord,
	selector string,
) (state.BoxRecord, error) {
	if box, exists := boxes[selector]; exists {
		return box, nil
	}
	matches := make(map[string]state.BoxRecord)
	for id, box := range boxes {
		if len(selector) >= 3 && strings.HasPrefix(id, selector) {
			matches[id] = box
		}
		if box.Name == selector {
			matches[id] = box
		}
	}
	switch len(matches) {
	case 0:
		return state.BoxRecord{}, fmt.Errorf(
			"%w: box %q",
			ErrSelectorNotFound,
			selector,
		)
	case 1:
		for _, box := range matches {
			return box, nil
		}
	default:
		return state.BoxRecord{}, fmt.Errorf(
			"%w: box %q matches %d boxes",
			ErrSelectorAmbiguous,
			selector,
			len(matches),
		)
	}
	panic("unreachable")
}

func resolveConnectionSelector(
	connections map[string]state.ConnectionRecord,
	selector string,
) (state.ConnectionRecord, error) {
	if record, exists := connections[selector]; exists {
		return record, nil
	}
	matches := make(map[string]state.ConnectionRecord)
	for id, record := range connections {
		if len(selector) >= 3 && strings.HasPrefix(id, selector) {
			matches[id] = record
		}
		if record.Name == selector && selector != "" {
			matches[id] = record
		}
	}
	switch len(matches) {
	case 0:
		return state.ConnectionRecord{}, fmt.Errorf(
			"%w: %q",
			ErrConnectionNotFound,
			selector,
		)
	case 1:
		for _, record := range matches {
			return record, nil
		}
	default:
		return state.ConnectionRecord{}, fmt.Errorf(
			"%w: connection %q matches %d connections",
			ErrSelectorAmbiguous,
			selector,
			len(matches),
		)
	}
	panic("unreachable")
}

func connectionNetworkName(id string) string {
	return "wktbox-connect-" + id
}

func containsMember(members []string, id string) bool {
	for _, member := range members {
		if member == id {
			return true
		}
	}
	return false
}

func sortedConnectionIDs(
	connections map[string]state.ConnectionRecord,
) []string {
	ids := make([]string, 0, len(connections))
	for id := range connections {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
