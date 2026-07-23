package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"wktbox/internal/compose"
	"wktbox/internal/lock"
	"wktbox/internal/ports"
	"wktbox/internal/state"
)

var ErrBoxNotFound = errors.New("box not found")

type Manager struct {
	backend compose.Backend
	store   state.Store
	locks   lock.Manager
	now     func() time.Time
}

func NewManager(
	backend compose.Backend,
	store state.Store,
	locks lock.Manager,
	now func() time.Time,
) Manager {
	return Manager{
		backend: backend,
		store:   store,
		locks:   locks,
		now:     now,
	}
}

func (manager Manager) Ensure(ctx context.Context, spec Spec) (state.BoxRecord, error) {
	return manager.ensure(ctx, spec, nil)
}

func (manager Manager) EnsureAllocated(
	ctx context.Context,
	spec Spec,
	allocator ports.Allocator,
) (state.BoxRecord, error) {
	return manager.ensure(ctx, spec, &allocator)
}

func (manager Manager) ensure(
	ctx context.Context,
	spec Spec,
	allocator *ports.Allocator,
) (state.BoxRecord, error) {
	unlock, err := manager.lockBox(ctx, spec.ID)
	if err != nil {
		return state.BoxRecord{}, err
	}
	defer unlock()

	current, err := manager.loadAndReconcile(ctx)
	if err != nil {
		return state.BoxRecord{}, err
	}
	existing, exists := current.Boxes[spec.ID]
	if spec.Ports.Size == 0 && exists && existing.Ports.Size != 0 {
		spec.Ports = existing.Ports
	}
	if spec.Ports.Size == 0 {
		if allocator == nil {
			return state.BoxRecord{}, errors.New("sandbox port block is required")
		}
		used := make([]ports.Block, 0, len(current.Boxes))
		for id, record := range current.Boxes {
			if id != spec.ID && record.Ports.Size != 0 {
				used = append(used, record.Ports)
			}
		}
		spec.Ports, err = allocator.Reserve(ctx, used)
		if err != nil {
			return state.BoxRecord{}, fmt.Errorf("reserve sandbox ports: %w", err)
		}
	}
	createdAt := manager.now().UTC()
	if exists && !existing.CreatedAt.IsZero() {
		createdAt = existing.CreatedAt
	}

	directory, err := manager.store.BoxDir(spec.ID)
	if err != nil {
		return state.BoxRecord{}, err
	}
	files, err := Render(directory, spec)
	if err != nil {
		return state.BoxRecord{}, err
	}
	requiresComposeApply := files.Changed ||
		(exists && existing.GatewayEnabled != spec.Config.Gateway.Enabled)
	record := existing
	record.ID = spec.ID
	if spec.Name != "" {
		record.Name = spec.Name
	} else if record.Name == "" {
		record.Name = filepath.Base(spec.Worktree)
	}
	if spec.Branch != "" {
		record.Branch = spec.Branch
	}
	record.Worktree = spec.Worktree
	record.ProjectName = "wktbox-" + spec.ID
	record.Ports = spec.Ports
	record.ComposePath = files.ComposePath
	record.SandboxEnvPath = files.SandboxEnvPath
	record.ProjectEnvOverridePath = files.ProjectEnvOverridePath
	record.GatewayEnabled = spec.Config.Gateway.Enabled
	record.CreatedAt = createdAt
	record.LastUsedAt = manager.now().UTC()

	project := projectFor(record)
	var realStatus compose.Status
	var inspectErr error
	if exists {
		realStatus, inspectErr = manager.backend.Inspect(ctx, project)
	}
	if exists && inspectErr == nil && realStatus.Ready() && !requiresComposeApply {
		record.Status = state.Ready
		current.Boxes[record.ID] = record
		if err := manager.store.Save(ctx, current); err != nil {
			return state.BoxRecord{}, err
		}
		return record, nil
	}

	if realStatus.Exists && realStatus.State == compose.Stopped {
		record.Status = state.Starting
	} else {
		record.Status = state.Creating
	}
	current.Boxes[record.ID] = record
	if err := manager.store.Save(ctx, current); err != nil {
		return state.BoxRecord{}, err
	}

	var lifecycleErr error
	if realStatus.Exists && realStatus.State == compose.Stopped && !requiresComposeApply {
		lifecycleErr = manager.backend.Start(ctx, project)
	} else {
		lifecycleErr = manager.backend.Up(ctx, project)
	}
	if lifecycleErr != nil {
		return state.BoxRecord{}, manager.persistError(ctx, current, record, lifecycleErr)
	}
	realStatus, err = manager.backend.Inspect(ctx, project)
	if err != nil {
		return state.BoxRecord{}, manager.persistError(ctx, current, record, err)
	}
	if !realStatus.Ready() {
		err = fmt.Errorf("box %s did not become ready", record.ID)
		return state.BoxRecord{}, manager.persistError(ctx, current, record, err)
	}

	record.Status = state.Ready
	current.Boxes[record.ID] = record
	if err := manager.store.Save(ctx, current); err != nil {
		return state.BoxRecord{}, err
	}
	return record, nil
}

func (manager Manager) Inspect(ctx context.Context, id string) (state.BoxRecord, error) {
	unlock, err := manager.lockBox(ctx, id)
	if err != nil {
		return state.BoxRecord{}, err
	}
	defer unlock()

	current, err := manager.loadAndReconcile(ctx)
	if err != nil {
		return state.BoxRecord{}, err
	}
	record, exists := current.Boxes[id]
	if !exists {
		return state.BoxRecord{}, fmt.Errorf("%w: %s", ErrBoxNotFound, id)
	}
	realStatus, err := manager.backend.Inspect(ctx, projectFor(record))
	if err != nil {
		return state.BoxRecord{}, err
	}
	record.Status = statusFromCompose(realStatus)
	current.Boxes[id] = record
	if err := manager.store.Save(ctx, current); err != nil {
		return state.BoxRecord{}, err
	}
	return record, nil
}

func (manager Manager) List(ctx context.Context) ([]state.BoxRecord, error) {
	unlock, err := manager.locks.Global(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()

	current, err := manager.loadAndReconcileUnlocked(ctx)
	if err != nil {
		return nil, err
	}
	records := make([]state.BoxRecord, 0, len(current.Boxes))
	for _, record := range current.Boxes {
		records = append(records, record)
	}
	sort.Slice(records, func(left int, right int) bool {
		return records[left].ID < records[right].ID
	})
	return records, nil
}

func (manager Manager) Stop(ctx context.Context, id string) error {
	unlock, err := manager.lockBox(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()

	current, err := manager.loadAndReconcile(ctx)
	if err != nil {
		return err
	}
	record, exists := current.Boxes[id]
	if !exists {
		return fmt.Errorf("%w: %s", ErrBoxNotFound, id)
	}
	record.Status = state.Stopping
	current.Boxes[id] = record
	if err := manager.store.Save(ctx, current); err != nil {
		return err
	}
	if err := manager.backend.Stop(ctx, projectFor(record)); err != nil {
		return manager.persistError(ctx, current, record, err)
	}
	record.Status = state.Stopped
	current.Boxes[id] = record
	return manager.store.Save(ctx, current)
}

func (manager Manager) Restart(ctx context.Context, id string) error {
	unlock, err := manager.lockBox(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()

	current, err := manager.loadAndReconcile(ctx)
	if err != nil {
		return err
	}
	record, exists := current.Boxes[id]
	if !exists {
		return fmt.Errorf("%w: %s", ErrBoxNotFound, id)
	}
	record.Status = state.Starting
	current.Boxes[id] = record
	if err := manager.store.Save(ctx, current); err != nil {
		return err
	}
	if err := manager.backend.Restart(ctx, projectFor(record)); err != nil {
		return manager.persistError(ctx, current, record, err)
	}
	realStatus, err := manager.backend.Inspect(ctx, projectFor(record))
	if err != nil {
		return manager.persistError(ctx, current, record, err)
	}
	if !realStatus.Ready() {
		return manager.persistError(
			ctx,
			current,
			record,
			fmt.Errorf("box %s did not become ready after restart", id),
		)
	}
	record.Status = state.Ready
	current.Boxes[id] = record
	return manager.store.Save(ctx, current)
}

func (manager Manager) Touch(ctx context.Context, id string) error {
	unlock, err := manager.lockBox(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()

	current, err := manager.loadAndReconcile(ctx)
	if err != nil {
		return err
	}
	record, exists := current.Boxes[id]
	if !exists {
		return fmt.Errorf("%w: %s", ErrBoxNotFound, id)
	}
	record.LastUsedAt = manager.now().UTC()
	current.Boxes[id] = record
	return manager.store.Save(ctx, current)
}

func (manager Manager) Destroy(ctx context.Context, id string) error {
	unlock, err := manager.lockBox(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()

	current, err := manager.loadAndReconcile(ctx)
	if err != nil {
		return err
	}
	record, exists := current.Boxes[id]
	if !exists {
		return fmt.Errorf("%w: %s", ErrBoxNotFound, id)
	}
	record.Status = state.Destroying
	current.Boxes[id] = record
	if err := manager.store.Save(ctx, current); err != nil {
		return err
	}
	if err := manager.backend.Down(ctx, projectFor(record), true); err != nil {
		return manager.persistError(ctx, current, record, err)
	}
	directory, err := manager.store.BoxDir(id)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(directory); err != nil {
		return fmt.Errorf("remove box files: %w", err)
	}
	delete(current.Boxes, id)
	return manager.store.Save(ctx, current)
}

func (manager Manager) lockBox(ctx context.Context, id string) (lock.UnlockFunc, error) {
	globalUnlock, err := manager.locks.Global(ctx)
	if err != nil {
		return nil, err
	}
	boxUnlock, err := manager.locks.Box(ctx, id)
	if err != nil {
		globalUnlock()
		return nil, err
	}
	return func() error {
		boxErr := boxUnlock()
		globalErr := globalUnlock()
		if boxErr != nil {
			return boxErr
		}
		return globalErr
	}, nil
}

func (manager Manager) loadAndReconcile(ctx context.Context) (state.State, error) {
	return manager.loadAndReconcileUnlocked(ctx)
}

func (manager Manager) loadAndReconcileUnlocked(ctx context.Context) (state.State, error) {
	current, err := manager.store.Load(ctx)
	if err != nil {
		return state.State{}, err
	}
	managed, err := manager.backend.ListManaged(ctx)
	if err != nil {
		return state.State{}, err
	}
	seen := make(map[string]bool, len(managed))
	for _, actual := range managed {
		seen[actual.ID] = true
		record, exists := current.Boxes[actual.ID]
		if !exists {
			directory, dirErr := manager.store.BoxDir(actual.ID)
			if dirErr != nil {
				return state.State{}, dirErr
			}
			record = state.BoxRecord{
				ID:             actual.ID,
				Name:           filepath.Base(actual.Worktree),
				CreatedAt:      manager.now().UTC(),
				LastUsedAt:     manager.now().UTC(),
				ComposePath:    filepath.Join(directory, "compose.yml"),
				SandboxEnvPath: filepath.Join(directory, "sandbox.env"),
			}
			overridePath := filepath.Join(directory, "project-env.override.yml")
			if _, statErr := os.Stat(overridePath); statErr == nil {
				record.ProjectEnvOverridePath = overridePath
			}
		}
		record.Worktree = actual.Worktree
		record.ProjectName = actual.ProjectName
		if actual.Ports.Size != 0 {
			record.Ports = actual.Ports
		}
		record.GatewayEnabled = actual.GatewayEnabled
		if actual.State == compose.Running && actual.Healthy {
			record.Status = state.Ready
		} else if actual.State == compose.Stopped {
			record.Status = state.Stopped
		} else {
			record.Status = state.Error
		}
		current.Boxes[actual.ID] = record
	}
	for id, record := range current.Boxes {
		if !seen[id] && record.Status != state.Creating {
			record.Status = state.Stopped
			current.Boxes[id] = record
		}
	}
	if err := manager.store.Save(ctx, current); err != nil {
		return state.State{}, err
	}
	return current, nil
}

func (manager Manager) persistError(
	ctx context.Context,
	current state.State,
	record state.BoxRecord,
	cause error,
) error {
	record.Status = state.Error
	current.Boxes[record.ID] = record
	if err := manager.store.Save(ctx, current); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func projectFor(record state.BoxRecord) compose.Project {
	files := []string{record.ComposePath}
	if record.ProjectEnvOverridePath != "" {
		files = append(files, record.ProjectEnvOverridePath)
	}
	return compose.Project{
		Name:           record.ProjectName,
		Files:          files,
		EnvFile:        record.SandboxEnvPath,
		GatewayEnabled: record.GatewayEnabled,
	}
}

func statusFromCompose(status compose.Status) state.Status {
	if status.Ready() {
		return state.Ready
	}
	if !status.Exists || status.State == compose.Stopped {
		return state.Stopped
	}
	return state.Error
}
