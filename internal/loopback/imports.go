package loopback

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"wktbox/internal/portforward"
)

type ImportApplyMode int

const (
	ImportApplyTransaction ImportApplyMode = iota
	ImportApplyEvent
)

var ErrImportPortConflict = errors.New(
	"workload localhost port is already in use",
)

var ErrImportWorkloadGone = errors.New(
	"workload disappeared after Docker snapshot",
)

type ImportProxySpec struct {
	Container Container
	Mapping   portforward.Mapping
}

type ImportProxy interface {
	Close() error
}

type ImportProxyFactory interface {
	Probe(context.Context, ImportProxySpec) error
	Start(context.Context, ImportProxySpec) (ImportProxy, error)
}

type ImportStopper interface {
	Stop(context.Context, string) error
}

type ImportOptions struct {
	Factory ImportProxyFactory
	Stopper ImportStopper
}

type activeImport struct {
	spec  ImportProxySpec
	proxy ImportProxy
}

type ImportReconciler struct {
	mutex    sync.Mutex
	factory  ImportProxyFactory
	stopper  ImportStopper
	active   map[string]activeImport
	desired  []portforward.Mapping
	warnings map[string]Warning
}

func NewImportReconciler(options ImportOptions) *ImportReconciler {
	return &ImportReconciler{
		factory:  options.Factory,
		stopper:  options.Stopper,
		active:   make(map[string]activeImport),
		warnings: make(map[string]Warning),
	}
}

func (reconciler *ImportReconciler) Preflight(
	ctx context.Context,
	containers []Container,
	mappings []portforward.Mapping,
) error {
	reconciler.mutex.Lock()
	defer reconciler.mutex.Unlock()
	if reconciler.factory == nil {
		return errors.New("import proxy factory is required")
	}
	for _, spec := range importSpecs(containers, mappings) {
		key := importKey(spec)
		if current, exists := reconciler.active[key]; exists &&
			equalImportSpec(current.spec, spec) {
			continue
		}
		if err := reconciler.factory.Probe(ctx, spec); err != nil {
			if errors.Is(err, ErrImportWorkloadGone) {
				continue
			}
			return importProbeError(spec, err)
		}
	}
	return nil
}

func (reconciler *ImportReconciler) Apply(
	ctx context.Context,
	containers []Container,
	mappings []portforward.Mapping,
	mode ImportApplyMode,
) ([]Warning, error) {
	reconciler.mutex.Lock()
	defer reconciler.mutex.Unlock()
	if reconciler.factory == nil {
		return nil, errors.New("import proxy factory is required")
	}
	candidateMappings := portforward.Filter(mappings, portforward.Import)
	reconciler.pruneWarnings(candidateMappings)
	specs := importSpecs(containers, candidateMappings)
	skippedContainers := make(map[string]bool)

	for _, spec := range specs {
		key := importKey(spec)
		if current, exists := reconciler.active[key]; exists &&
			equalImportSpec(current.spec, spec) {
			delete(reconciler.warnings, key)
			continue
		}
		if err := reconciler.factory.Probe(ctx, spec); err != nil {
			if errors.Is(err, ErrImportWorkloadGone) {
				skippedContainers[spec.Container.ID] = true
				continue
			}
			if !errors.Is(err, ErrImportPortConflict) {
				return nil, importProbeError(spec, err)
			}
			if mode == ImportApplyTransaction {
				return nil, importConflict(spec, err)
			}
			if skippedContainers[spec.Container.ID] {
				continue
			}
			if reconciler.stopper == nil {
				return nil, fmt.Errorf(
					"stop conflicting workload %s: stopper is not configured",
					spec.Container.Name,
				)
			}
			if stopErr := reconciler.stopper.Stop(ctx, spec.Container.ID); stopErr != nil {
				return nil, errors.Join(importConflict(spec, err), stopErr)
			}
			skippedContainers[spec.Container.ID] = true
			reconciler.warnings[key] = importWarning(spec)
			continue
		}
		delete(reconciler.warnings, key)
	}

	started := make(map[string]activeImport)
	for _, spec := range specs {
		if skippedContainers[spec.Container.ID] {
			continue
		}
		key := importKey(spec)
		if current, exists := reconciler.active[key]; exists &&
			equalImportSpec(current.spec, spec) {
			continue
		}
		proxy, err := reconciler.factory.Start(ctx, spec)
		if err != nil {
			closeActiveImports(started)
			return nil, fmt.Errorf(
				"start import %q on workload %s localhost:%d: %w",
				spec.Mapping.Name,
				spec.Container.Name,
				spec.Mapping.TargetPort,
				err,
			)
		}
		started[key] = activeImport{spec: spec, proxy: proxy}
	}

	next := make(map[string]activeImport, len(specs))
	for _, spec := range specs {
		if skippedContainers[spec.Container.ID] {
			continue
		}
		key := importKey(spec)
		if current, exists := reconciler.active[key]; exists &&
			equalImportSpec(current.spec, spec) {
			next[key] = current
			continue
		}
		next[key] = started[key]
	}
	for key, current := range reconciler.active {
		if _, keep := next[key]; !keep {
			_ = current.proxy.Close()
		}
	}
	reconciler.active = next
	reconciler.desired = append([]portforward.Mapping(nil), candidateMappings...)
	return reconciler.currentWarnings(), nil
}

func (reconciler *ImportReconciler) Desired() []portforward.Mapping {
	reconciler.mutex.Lock()
	defer reconciler.mutex.Unlock()
	return append([]portforward.Mapping(nil), reconciler.desired...)
}

func (reconciler *ImportReconciler) Close() error {
	reconciler.mutex.Lock()
	defer reconciler.mutex.Unlock()
	var result error
	for _, current := range reconciler.active {
		result = errors.Join(result, current.proxy.Close())
	}
	reconciler.active = make(map[string]activeImport)
	reconciler.warnings = make(map[string]Warning)
	return result
}

func (reconciler *ImportReconciler) pruneWarnings(
	mappings []portforward.Mapping,
) {
	desiredNames := make(map[string]bool, len(mappings))
	for _, mapping := range mappings {
		desiredNames[mapping.Name] = true
	}
	for key := range reconciler.warnings {
		separator := strings.LastIndexByte(key, '\x00')
		if separator < 0 || !desiredNames[key[separator+1:]] {
			delete(reconciler.warnings, key)
		}
	}
}

func (reconciler *ImportReconciler) currentWarnings() []Warning {
	warnings := make([]Warning, 0, len(reconciler.warnings))
	for _, warning := range reconciler.warnings {
		warnings = append(warnings, warning)
	}
	sort.Slice(warnings, func(left int, right int) bool {
		if warnings[left].Source != warnings[right].Source {
			return warnings[left].Source < warnings[right].Source
		}
		if warnings[left].Port != warnings[right].Port {
			return warnings[left].Port < warnings[right].Port
		}
		return warnings[left].Message < warnings[right].Message
	})
	return warnings
}

func importSpecs(
	containers []Container,
	mappings []portforward.Mapping,
) []ImportProxySpec {
	sortedContainers := append([]Container(nil), containers...)
	sort.Slice(sortedContainers, func(left int, right int) bool {
		if sortedContainers[left].Name != sortedContainers[right].Name {
			return sortedContainers[left].Name < sortedContainers[right].Name
		}
		return sortedContainers[left].ID < sortedContainers[right].ID
	})
	imports := portforward.Filter(mappings, portforward.Import)
	specs := make([]ImportProxySpec, 0, len(sortedContainers)*len(imports))
	for _, container := range sortedContainers {
		if !container.Running || container.PID <= 0 ||
			container.Labels["io.wktbox.port-helper"] == "true" {
			continue
		}
		for _, mapping := range imports {
			specs = append(specs, ImportProxySpec{
				Container: container,
				Mapping:   mapping,
			})
		}
	}
	return specs
}

func importKey(spec ImportProxySpec) string {
	return spec.Container.ID + "\x00" + spec.Mapping.Name
}

func equalImportSpec(left ImportProxySpec, right ImportProxySpec) bool {
	return left.Container.ID == right.Container.ID &&
		left.Container.PID == right.Container.PID &&
		left.Mapping.Name == right.Mapping.Name &&
		left.Mapping.SourceAddress == right.Mapping.SourceAddress &&
		left.Mapping.SourcePort == right.Mapping.SourcePort &&
		left.Mapping.TargetPort == right.Mapping.TargetPort
}

func importConflict(spec ImportProxySpec, cause error) error {
	return fmt.Errorf(
		"workload %s already uses localhost:%d required by import %q: %w",
		spec.Container.Name,
		spec.Mapping.TargetPort,
		spec.Mapping.Name,
		cause,
	)
}

func importProbeError(spec ImportProxySpec, cause error) error {
	if errors.Is(cause, ErrImportPortConflict) {
		return importConflict(spec, cause)
	}
	return fmt.Errorf(
		"check localhost:%d for import %q on workload %s: %w",
		spec.Mapping.TargetPort,
		spec.Mapping.Name,
		spec.Container.Name,
		cause,
	)
}

func importWarning(spec ImportProxySpec) Warning {
	return Warning{
		Code:   "port_import_conflict",
		Port:   spec.Mapping.TargetPort,
		Source: spec.Container.Name,
		Message: fmt.Sprintf(
			"workload stopped because localhost:%d is reserved by import %q",
			spec.Mapping.TargetPort,
			spec.Mapping.Name,
		),
	}
}

func closeActiveImports(active map[string]activeImport) {
	for _, current := range active {
		_ = current.proxy.Close()
	}
}
