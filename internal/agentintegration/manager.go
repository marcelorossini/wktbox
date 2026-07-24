package agentintegration

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Manager struct {
	homeDir   func() (string, error)
	lookupEnv func(string) (string, bool)
	assets    fs.FS
	version   string
	writeFile func(string, []byte, fs.FileMode) error
}

func NewManager(dependencies Dependencies) *Manager {
	homeDir := dependencies.HomeDir
	if homeDir == nil {
		homeDir = os.UserHomeDir
	}
	lookupEnv := dependencies.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	writeFile := dependencies.WriteFile
	if writeFile == nil {
		writeFile = syncWriteFile
	}
	assets := dependencies.Assets
	if assets == nil {
		assets = BundledAssets()
	}
	return &Manager{
		homeDir:   homeDir,
		lookupEnv: lookupEnv,
		assets:    assets,
		version:   dependencies.Version,
		writeFile: writeFile,
	}
}

func (manager *Manager) Install(
	ctx context.Context,
	options Options,
) (Report, error) {
	targets, err := expandTargets(options.Target)
	if err != nil {
		return Report{}, err
	}
	report := Report{Targets: make([]TargetStatus, 0, len(targets))}
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		status, err := manager.installTarget(target, options)
		report.Targets = append(report.Targets, status)
		if err != nil {
			return report, err
		}
	}
	return report, nil
}

func (manager *Manager) Uninstall(
	ctx context.Context,
	options Options,
) (Report, error) {
	targets, err := expandTargets(options.Target)
	if err != nil {
		return Report{}, err
	}
	report := Report{Targets: make([]TargetStatus, 0, len(targets))}
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		status, err := manager.uninstallTarget(target, options)
		report.Targets = append(report.Targets, status)
		if err != nil {
			return report, err
		}
	}
	return report, nil
}

func (manager *Manager) Status(
	ctx context.Context,
	options Options,
) (Report, error) {
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	targets, err := expandTargets(options.Target)
	if err != nil {
		return Report{}, err
	}
	report := Report{Targets: make([]TargetStatus, 0, len(targets))}
	for _, target := range targets {
		status, err := manager.status(target)
		if err != nil {
			return Report{}, err
		}
		report.Targets = append(report.Targets, status)
	}
	return report, nil
}

func (manager *Manager) status(target Target) (TargetStatus, error) {
	paths, err := manager.paths(target)
	if err != nil {
		return TargetStatus{}, err
	}
	if manager.assets == nil {
		return TargetStatus{}, fmt.Errorf("agent skill assets are not configured")
	}
	expectedDigest, err := digestTree(manager.assets, SkillName)
	if err != nil {
		return TargetStatus{}, fmt.Errorf("digest bundled skill: %w", err)
	}
	status := TargetStatus{
		Target:           target,
		SkillPath:        paths.skill,
		InstructionsPath: paths.instructions,
		ExpectedDigest:   expectedDigest,
	}

	info, err := os.Stat(paths.skill)
	switch {
	case err == nil && !info.IsDir():
		return TargetStatus{}, fmt.Errorf(
			"installed skill path is not a directory: %s",
			paths.skill,
		)
	case err == nil:
		status.Installed = true
		status.Version = manager.version
		status.ActualDigest, err = digestTree(os.DirFS(paths.skill), ".")
		if err != nil {
			return TargetStatus{}, fmt.Errorf("digest installed skill: %w", err)
		}
		status.Conflict = status.ActualDigest != status.ExpectedDigest
	case os.IsNotExist(err):
	case err != nil:
		return TargetStatus{}, fmt.Errorf("inspect installed skill: %w", err)
	}

	instructions, err := os.ReadFile(paths.instructions)
	switch {
	case err == nil:
		status.ManagedBlock = strings.Contains(
			string(instructions),
			ManagedInstructionsBlock,
		)
	case os.IsNotExist(err):
	case err != nil:
		return TargetStatus{}, fmt.Errorf("read agent instructions: %w", err)
	}
	return status, nil
}

func (manager *Manager) installTarget(
	target Target,
	options Options,
) (TargetStatus, error) {
	current, err := manager.status(target)
	if err != nil {
		return TargetStatus{}, err
	}
	if current.Conflict && !options.Force {
		return current, fmt.Errorf("%w: %s", ErrConflict, target)
	}
	installSkill := !current.Installed || current.Conflict
	installInstructions := !current.ManagedBlock
	current.Changed = installSkill || installInstructions
	if !current.Changed || options.DryRun {
		return current, nil
	}

	paths, err := manager.paths(target)
	if err != nil {
		return current, err
	}
	var stagedSkill string
	if installSkill {
		if err := os.MkdirAll(filepath.Dir(paths.skill), 0o700); err != nil {
			return current, fmt.Errorf("create skill parent: %w", err)
		}
		stagedSkill, err = os.MkdirTemp(
			filepath.Dir(paths.skill),
			".wktbox-skill-*",
		)
		if err != nil {
			return current, fmt.Errorf("stage skill: %w", err)
		}
		defer os.RemoveAll(stagedSkill)
		if err := copySkillTree(
			manager.assets,
			SkillName,
			stagedSkill,
			manager.writeFile,
		); err != nil {
			return current, fmt.Errorf("stage skill: %w", err)
		}
	}

	var stagedInstructions string
	if installInstructions {
		existing, err := readFileOrEmpty(paths.instructions)
		if err != nil {
			return current, fmt.Errorf("read agent instructions: %w", err)
		}
		rendered, err := renderManagedInstructions(string(existing))
		if err != nil {
			return current, err
		}
		stagedInstructions, err = prepareAtomicFile(
			paths.instructions,
			[]byte(rendered),
			0o600,
		)
		if err != nil {
			return current, fmt.Errorf("stage agent instructions: %w", err)
		}
		defer os.Remove(stagedInstructions)
	}

	backupSkill := ""
	if installSkill && current.Installed {
		backupSkill = filepath.Join(
			filepath.Dir(paths.skill),
			".wktbox-backup-"+filepath.Base(stagedSkill),
		)
		if err := os.Rename(paths.skill, backupSkill); err != nil {
			return current, fmt.Errorf("backup installed skill: %w", err)
		}
	}
	skillCommitted := false
	if installSkill {
		if err := os.Rename(stagedSkill, paths.skill); err != nil {
			if backupSkill != "" {
				_ = os.Rename(backupSkill, paths.skill)
			}
			return current, fmt.Errorf("activate staged skill: %w", err)
		}
		skillCommitted = true
		stagedSkill = ""
	}
	rollbackSkill := func() {
		if !skillCommitted {
			return
		}
		_ = os.RemoveAll(paths.skill)
		if backupSkill != "" {
			_ = os.Rename(backupSkill, paths.skill)
		}
	}

	if installInstructions {
		if err := os.Rename(stagedInstructions, paths.instructions); err != nil {
			rollbackSkill()
			return current, fmt.Errorf("activate agent instructions: %w", err)
		}
		stagedInstructions = ""
	}
	if backupSkill != "" {
		if err := os.RemoveAll(backupSkill); err != nil {
			return current, fmt.Errorf("remove skill backup: %w", err)
		}
	}
	if installSkill {
		if err := syncDirectory(filepath.Dir(paths.skill)); err != nil {
			return current, err
		}
	}
	if installInstructions {
		if err := syncDirectory(filepath.Dir(paths.instructions)); err != nil {
			return current, err
		}
	}

	installed, err := manager.status(target)
	if err != nil {
		return current, err
	}
	installed.Changed = true
	return installed, nil
}

func (manager *Manager) uninstallTarget(
	target Target,
	options Options,
) (TargetStatus, error) {
	current, err := manager.status(target)
	if err != nil {
		return TargetStatus{}, err
	}
	if current.Conflict && !options.Force {
		return current, fmt.Errorf("%w: %s", ErrConflict, target)
	}
	paths, err := manager.paths(target)
	if err != nil {
		return current, err
	}
	existingInstructions, err := readFileOrEmpty(paths.instructions)
	if err != nil {
		return current, fmt.Errorf("read agent instructions: %w", err)
	}
	hasManagedMarkers := strings.Contains(
		string(existingInstructions),
		managedStartMarker,
	) || strings.Contains(string(existingInstructions), managedEndMarker)
	renderedInstructions := string(existingInstructions)
	if hasManagedMarkers {
		renderedInstructions, err = removeManagedInstructions(
			string(existingInstructions),
		)
		if err != nil {
			return current, err
		}
	}

	current.Changed = current.Installed || hasManagedMarkers
	if !current.Changed || options.DryRun {
		return current, nil
	}

	stagedInstructions := ""
	if hasManagedMarkers && renderedInstructions != "" {
		stagedInstructions, err = prepareAtomicFile(
			paths.instructions,
			[]byte(renderedInstructions),
			0o600,
		)
		if err != nil {
			return current, fmt.Errorf("stage agent instructions: %w", err)
		}
		defer os.Remove(stagedInstructions)
	}

	skillBackup := ""
	if current.Installed {
		skillBackup, err = reserveSiblingPath(
			filepath.Dir(paths.skill),
			".wktbox-removed-skill-*",
		)
		if err != nil {
			return current, fmt.Errorf("reserve skill backup: %w", err)
		}
		if err := os.Rename(paths.skill, skillBackup); err != nil {
			return current, fmt.Errorf("remove installed skill: %w", err)
		}
	}
	rollbackSkill := func() {
		if skillBackup != "" {
			_ = os.Rename(skillBackup, paths.skill)
		}
	}

	instructionsBackup := ""
	if hasManagedMarkers {
		instructionsBackup, err = reserveSiblingPath(
			filepath.Dir(paths.instructions),
			".wktbox-removed-instructions-*",
		)
		if err != nil {
			rollbackSkill()
			return current, fmt.Errorf("reserve instructions backup: %w", err)
		}
		if err := os.Rename(paths.instructions, instructionsBackup); err != nil {
			rollbackSkill()
			return current, fmt.Errorf("backup agent instructions: %w", err)
		}
		if stagedInstructions != "" {
			if err := os.Rename(stagedInstructions, paths.instructions); err != nil {
				_ = os.Rename(instructionsBackup, paths.instructions)
				rollbackSkill()
				return current, fmt.Errorf(
					"activate agent instructions: %w",
					err,
				)
			}
			stagedInstructions = ""
		}
	}

	if skillBackup != "" {
		if err := os.RemoveAll(skillBackup); err != nil {
			return current, fmt.Errorf("delete removed skill: %w", err)
		}
	}
	if instructionsBackup != "" {
		if err := os.Remove(instructionsBackup); err != nil {
			return current, fmt.Errorf("delete instructions backup: %w", err)
		}
	}

	skillsParent := filepath.Dir(paths.skill)
	if err := os.Remove(skillsParent); err != nil && !os.IsNotExist(err) {
		entries, readErr := os.ReadDir(skillsParent)
		if readErr != nil {
			return current, fmt.Errorf(
				"inspect skills directory after removal failure: %w",
				readErr,
			)
		}
		if len(entries) == 0 {
			return current, fmt.Errorf("remove empty skills directory: %w", err)
		}
	}
	if _, err := os.Stat(skillsParent); os.IsNotExist(err) {
		if err := syncDirectory(filepath.Dir(skillsParent)); err != nil {
			return current, err
		}
	} else if err == nil {
		if err := syncDirectory(skillsParent); err != nil {
			return current, err
		}
	} else {
		return current, err
	}
	if hasManagedMarkers {
		if err := syncDirectory(filepath.Dir(paths.instructions)); err != nil {
			return current, err
		}
	}

	uninstalled, err := manager.status(target)
	if err != nil {
		return current, err
	}
	uninstalled.Changed = true
	return uninstalled, nil
}

func (manager *Manager) paths(target Target) (targetPaths, error) {
	home, err := manager.homeDir()
	if err != nil {
		return targetPaths{}, fmt.Errorf("resolve home directory: %w", err)
	}
	switch target {
	case TargetCodex:
		codexHome := manager.environmentDirectory(
			"CODEX_HOME",
			filepath.Join(home, ".codex"),
		)
		return targetPaths{
			skill: filepath.Join(
				home,
				".agents",
				"skills",
				SkillName,
			),
			instructions: filepath.Join(codexHome, "AGENTS.md"),
		}, nil
	case TargetClaude:
		claudeHome := manager.environmentDirectory(
			"CLAUDE_CONFIG_DIR",
			filepath.Join(home, ".claude"),
		)
		return targetPaths{
			skill:        filepath.Join(claudeHome, "skills", SkillName),
			instructions: filepath.Join(claudeHome, "CLAUDE.md"),
		}, nil
	default:
		return targetPaths{}, fmt.Errorf("unsupported agent target %q", target)
	}
}

func (manager *Manager) environmentDirectory(name string, fallback string) string {
	if value, ok := manager.lookupEnv(name); ok && strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func expandTargets(target Target) ([]Target, error) {
	if target == "" {
		target = TargetAll
	}
	switch target {
	case TargetAll:
		return []Target{TargetCodex, TargetClaude}, nil
	case TargetCodex, TargetClaude:
		return []Target{target}, nil
	default:
		return nil, fmt.Errorf("unsupported agent target %q", target)
	}
}
