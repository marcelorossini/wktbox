package agentintegration_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"wktbox/internal/agentintegration"
)

const managedBlock = `<!-- wktbox-agent:start -->
When development needs Docker isolation, independent Compose ports, browser or
integration testing, use the ` + "`wktbox-isolated-development`" + ` skill. It
works in any existing project directory; Git and linked worktrees are optional.
<!-- wktbox-agent:end -->`

func TestCodexPathsUseDefaultsAndCodexHomeOverride(t *testing.T) {
	home := t.TempDir()
	manager := newManager(t, home, nil)

	report, err := manager.Status(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	)
	if err != nil {
		t.Fatal(err)
	}
	status := report.Targets[0]
	assertPath(
		t,
		status.SkillPath,
		filepath.Join(
			home,
			".agents",
			"skills",
			"wktbox-isolated-development",
		),
	)
	assertPath(
		t,
		status.InstructionsPath,
		filepath.Join(home, ".codex", "AGENTS.md"),
	)

	codexHome := filepath.Join(home, "custom-codex")
	manager = newManager(t, home, map[string]string{
		"CODEX_HOME": codexHome,
	})
	report, err = manager.Status(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertPath(t, report.Targets[0].InstructionsPath, filepath.Join(
		codexHome,
		"AGENTS.md",
	))
}

func TestClaudePathsUseDefaultsAndConfigOverride(t *testing.T) {
	home := t.TempDir()
	manager := newManager(t, home, nil)

	report, err := manager.Status(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetClaude},
	)
	if err != nil {
		t.Fatal(err)
	}
	status := report.Targets[0]
	assertPath(
		t,
		status.SkillPath,
		filepath.Join(
			home,
			".claude",
			"skills",
			"wktbox-isolated-development",
		),
	)
	assertPath(
		t,
		status.InstructionsPath,
		filepath.Join(home, ".claude", "CLAUDE.md"),
	)

	configDir := filepath.Join(home, "custom-claude")
	manager = newManager(t, home, map[string]string{
		"CLAUDE_CONFIG_DIR": configDir,
	})
	report, err = manager.Status(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetClaude},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertPath(
		t,
		report.Targets[0].SkillPath,
		filepath.Join(
			configDir,
			"skills",
			"wktbox-isolated-development",
		),
	)
	assertPath(
		t,
		report.Targets[0].InstructionsPath,
		filepath.Join(configDir, "CLAUDE.md"),
	)
}

func TestAllExpandsInStableCodexClaudeOrder(t *testing.T) {
	manager := newManager(t, t.TempDir(), nil)

	report, err := manager.Status(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetAll},
	)
	if err != nil {
		t.Fatal(err)
	}
	got := []agentintegration.Target{
		report.Targets[0].Target,
		report.Targets[1].Target,
	}
	want := []agentintegration.Target{
		agentintegration.TargetCodex,
		agentintegration.TargetClaude,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targets = %#v, want %#v", got, want)
	}
}

func TestStatusReportsMissingInstallation(t *testing.T) {
	manager := newManager(t, t.TempDir(), nil)

	report, err := manager.Status(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	)
	if err != nil {
		t.Fatal(err)
	}
	status := report.Targets[0]
	if status.Installed || status.ManagedBlock || status.Conflict {
		t.Fatalf("status = %#v", status)
	}
	if status.ExpectedDigest == "" || status.ActualDigest != "" {
		t.Fatalf("digests = %#v", status)
	}
}

func TestStatusReportsMatchingDigestAndManagedBlock(t *testing.T) {
	home := t.TempDir()
	manager := newManager(t, home, nil)
	status := statusFor(t, manager, agentintegration.TargetCodex)
	writeCanonicalSkill(t, status.SkillPath)
	writeTestFile(t, status.InstructionsPath, "user text\n\n"+managedBlock+"\n")

	report, err := manager.Status(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	)
	if err != nil {
		t.Fatal(err)
	}
	got := report.Targets[0]
	if !got.Installed || !got.ManagedBlock || got.Conflict {
		t.Fatalf("status = %#v", got)
	}
	if got.ActualDigest != got.ExpectedDigest {
		t.Fatalf(
			"actual digest = %q, expected %q",
			got.ActualDigest,
			got.ExpectedDigest,
		)
	}
}

func TestStatusReportsModifiedSkillConflict(t *testing.T) {
	home := t.TempDir()
	manager := newManager(t, home, nil)
	status := statusFor(t, manager, agentintegration.TargetCodex)
	writeCanonicalSkill(t, status.SkillPath)
	writeTestFile(
		t,
		filepath.Join(status.SkillPath, "SKILL.md"),
		"locally modified\n",
	)

	got := statusFor(t, manager, agentintegration.TargetCodex)

	if !got.Installed || !got.Conflict {
		t.Fatalf("status = %#v", got)
	}
	if got.ActualDigest == got.ExpectedDigest {
		t.Fatalf("digests unexpectedly match: %#v", got)
	}
}

func TestStatusReportsMissingManagedBlockIndependently(t *testing.T) {
	home := t.TempDir()
	manager := newManager(t, home, nil)
	status := statusFor(t, manager, agentintegration.TargetCodex)
	writeCanonicalSkill(t, status.SkillPath)
	writeTestFile(t, status.InstructionsPath, "user-owned instructions\n")

	got := statusFor(t, manager, agentintegration.TargetCodex)

	if !got.Installed || got.ManagedBlock || got.Conflict {
		t.Fatalf("status = %#v", got)
	}
}

func TestInstallCreatesSkillAndPreservesRequiredModes(t *testing.T) {
	home := t.TempDir()
	manager := newManager(t, home, nil)

	report, err := manager.Install(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	)
	if err != nil {
		t.Fatal(err)
	}
	status := report.Targets[0]
	if !status.Installed || !status.ManagedBlock || !status.Changed {
		t.Fatalf("status = %#v", status)
	}
	if status.ActualDigest != status.ExpectedDigest {
		t.Fatalf("digests = %#v", status)
	}
	assertMode(t, status.SkillPath, 0o700)
	assertMode(t, filepath.Join(status.SkillPath, "SKILL.md"), 0o644)
	assertMode(
		t,
		filepath.Join(status.SkillPath, "agents", "openai.yaml"),
		0o644,
	)
	assertMode(t, status.InstructionsPath, 0o600)
}

func TestInstallUsesBundledAssetsByDefault(t *testing.T) {
	home := t.TempDir()
	manager := agentintegration.NewManager(agentintegration.Dependencies{
		HomeDir: func() (string, error) {
			return home, nil
		},
		LookupEnv: func(string) (string, bool) {
			return "", false
		},
		Version: "0.1.0",
	})

	report, err := manager.Install(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	)
	if err != nil {
		t.Fatal(err)
	}
	skill := readTestFile(
		t,
		filepath.Join(report.Targets[0].SkillPath, "SKILL.md"),
	)
	if !strings.Contains(skill, "wktbox-isolated-development") ||
		!strings.Contains(skill, "wktbox doctor --path <workspace>") ||
		!strings.Contains(skill, "wktbox run --path <workspace> --") {
		t.Fatalf("skill = %q", skill)
	}
	if report.Targets[0].ActualDigest != report.Targets[0].ExpectedDigest {
		t.Fatalf("status = %#v", report.Targets[0])
	}
}

func TestInstallSecondRunIsNoOp(t *testing.T) {
	manager := newManager(t, t.TempDir(), nil)
	if _, err := manager.Install(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	); err != nil {
		t.Fatal(err)
	}

	report, err := manager.Install(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Targets[0].Changed {
		t.Fatalf("status = %#v", report.Targets[0])
	}
}

func TestInstallDryRunReportsChangeWithoutWriting(t *testing.T) {
	manager := newManager(t, t.TempDir(), nil)
	before := statusFor(t, manager, agentintegration.TargetCodex)

	report, err := manager.Install(
		context.Background(),
		agentintegration.Options{
			Target: agentintegration.TargetCodex,
			DryRun: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Targets[0].Changed {
		t.Fatalf("status = %#v", report.Targets[0])
	}
	if _, err := os.Stat(before.SkillPath); !os.IsNotExist(err) {
		t.Fatalf("skill stat error = %v", err)
	}
	if _, err := os.Stat(before.InstructionsPath); !os.IsNotExist(err) {
		t.Fatalf("instructions stat error = %v", err)
	}
}

func TestInstallPreservesUserInstructionsAndExistingMode(t *testing.T) {
	home := t.TempDir()
	manager := newManager(t, home, nil)
	status := statusFor(t, manager, agentintegration.TargetCodex)
	writeTestFile(
		t,
		status.InstructionsPath,
		"# Personal instructions\n\nKeep this exact text.\n",
	)
	if err := os.Chmod(status.InstructionsPath, 0o640); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.Install(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	); err != nil {
		t.Fatal(err)
	}

	body := readTestFile(t, status.InstructionsPath)
	if !strings.Contains(body, "# Personal instructions") ||
		!strings.Contains(body, "Keep this exact text.") ||
		!strings.Contains(body, managedBlock) {
		t.Fatalf("instructions = %q", body)
	}
	assertMode(t, status.InstructionsPath, 0o640)
}

func TestInstallReplacesOnlyExistingManagedBlock(t *testing.T) {
	home := t.TempDir()
	manager := newManager(t, home, nil)
	status := statusFor(t, manager, agentintegration.TargetCodex)
	writeTestFile(
		t,
		status.InstructionsPath,
		"before\n<!-- wktbox-agent:start -->\nold text\n"+
			"<!-- wktbox-agent:end -->\nafter\n",
	)

	if _, err := manager.Install(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	); err != nil {
		t.Fatal(err)
	}

	body := readTestFile(t, status.InstructionsPath)
	if strings.Contains(body, "old text") ||
		!strings.HasPrefix(body, "before\n") ||
		!strings.HasSuffix(body, "\nafter\n") ||
		strings.Count(body, "<!-- wktbox-agent:start -->") != 1 {
		t.Fatalf("instructions = %q", body)
	}
}

func TestInstallRefusesModifiedSkillWithoutForce(t *testing.T) {
	manager := newManager(t, t.TempDir(), nil)
	report, err := manager.Install(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	)
	if err != nil {
		t.Fatal(err)
	}
	skillFile := filepath.Join(report.Targets[0].SkillPath, "SKILL.md")
	writeTestFile(t, skillFile, "local changes\n")

	_, err = manager.Install(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	)

	if !errors.Is(err, agentintegration.ErrConflict) {
		t.Fatalf("error = %v", err)
	}
	if got := readTestFile(t, skillFile); got != "local changes\n" {
		t.Fatalf("skill = %q", got)
	}
}

func TestInstallForceReplacesModifiedSkill(t *testing.T) {
	manager := newManager(t, t.TempDir(), nil)
	report, err := manager.Install(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	)
	if err != nil {
		t.Fatal(err)
	}
	skillFile := filepath.Join(report.Targets[0].SkillPath, "SKILL.md")
	writeTestFile(t, skillFile, "local changes\n")

	report, err = manager.Install(
		context.Background(),
		agentintegration.Options{
			Target: agentintegration.TargetCodex,
			Force:  true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	status := report.Targets[0]
	if status.Conflict || !status.Changed ||
		status.ActualDigest != status.ExpectedDigest {
		t.Fatalf("status = %#v", status)
	}
}

func TestInstallDoesNotLeavePartialTreeAfterStagingWriteFailure(t *testing.T) {
	home := t.TempDir()
	manager := newManagerWithDependencies(t, home, nil, func(
		path string,
		data []byte,
		mode fs.FileMode,
	) error {
		if filepath.Base(path) == "openai.yaml" {
			return errors.New("injected write failure")
		}
		return os.WriteFile(path, data, mode)
	})
	status := statusFor(t, manager, agentintegration.TargetCodex)

	_, err := manager.Install(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	)

	if err == nil || !strings.Contains(err.Error(), "injected write failure") {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(status.SkillPath); !os.IsNotExist(statErr) {
		t.Fatalf("skill stat error = %v", statErr)
	}
}

func TestUninstallRemovesCleanSkillAndManagedBlock(t *testing.T) {
	manager := newManager(t, t.TempDir(), nil)
	installed, err := manager.Install(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	)
	if err != nil {
		t.Fatal(err)
	}

	report, err := manager.Uninstall(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	)
	if err != nil {
		t.Fatal(err)
	}
	status := report.Targets[0]
	if status.Installed || status.ManagedBlock || !status.Changed {
		t.Fatalf("status = %#v", status)
	}
	if _, err := os.Stat(installed.Targets[0].SkillPath); !os.IsNotExist(err) {
		t.Fatalf("skill stat error = %v", err)
	}
}

func TestUninstallPreservesUserTextOutsideManagedBlock(t *testing.T) {
	home := t.TempDir()
	manager := newManager(t, home, nil)
	status := statusFor(t, manager, agentintegration.TargetCodex)
	writeTestFile(
		t,
		status.InstructionsPath,
		"# Personal\n\n"+managedBlock+"\n\nKeep this too.\n",
	)
	writeCanonicalSkill(t, status.SkillPath)

	if _, err := manager.Uninstall(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	); err != nil {
		t.Fatal(err)
	}

	body := readTestFile(t, status.InstructionsPath)
	if !strings.Contains(body, "# Personal") ||
		!strings.Contains(body, "Keep this too.") ||
		strings.Contains(body, "wktbox-agent") {
		t.Fatalf("instructions = %q", body)
	}
}

func TestUninstallAbsentInstallationIsNoOp(t *testing.T) {
	manager := newManager(t, t.TempDir(), nil)

	report, err := manager.Uninstall(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Targets[0].Changed {
		t.Fatalf("status = %#v", report.Targets[0])
	}
}

func TestUninstallRefusesModifiedSkillWithoutForce(t *testing.T) {
	manager := newManager(t, t.TempDir(), nil)
	report, err := manager.Install(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	)
	if err != nil {
		t.Fatal(err)
	}
	skillFile := filepath.Join(report.Targets[0].SkillPath, "SKILL.md")
	writeTestFile(t, skillFile, "local changes\n")

	_, err = manager.Uninstall(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	)

	if !errors.Is(err, agentintegration.ErrConflict) {
		t.Fatalf("error = %v", err)
	}
	if got := readTestFile(t, skillFile); got != "local changes\n" {
		t.Fatalf("skill = %q", got)
	}
}

func TestUninstallForceRemovesModifiedSkill(t *testing.T) {
	manager := newManager(t, t.TempDir(), nil)
	report, err := manager.Install(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	)
	if err != nil {
		t.Fatal(err)
	}
	skillFile := filepath.Join(report.Targets[0].SkillPath, "SKILL.md")
	writeTestFile(t, skillFile, "local changes\n")

	report, err = manager.Uninstall(
		context.Background(),
		agentintegration.Options{
			Target: agentintegration.TargetCodex,
			Force:  true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if report.Targets[0].Installed || !report.Targets[0].Changed {
		t.Fatalf("status = %#v", report.Targets[0])
	}
}

func TestUninstallDryRunDoesNotRemoveFiles(t *testing.T) {
	manager := newManager(t, t.TempDir(), nil)
	installed, err := manager.Install(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	)
	if err != nil {
		t.Fatal(err)
	}

	report, err := manager.Uninstall(
		context.Background(),
		agentintegration.Options{
			Target: agentintegration.TargetCodex,
			DryRun: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Targets[0].Changed {
		t.Fatalf("status = %#v", report.Targets[0])
	}
	if _, err := os.Stat(installed.Targets[0].SkillPath); err != nil {
		t.Fatalf("skill stat error = %v", err)
	}
}

func TestUninstallRemovesEmptySkillsParent(t *testing.T) {
	manager := newManager(t, t.TempDir(), nil)
	installed, err := manager.Install(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	)
	if err != nil {
		t.Fatal(err)
	}
	skillsParent := filepath.Dir(installed.Targets[0].SkillPath)

	if _, err := manager.Uninstall(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(skillsParent); !os.IsNotExist(err) {
		t.Fatalf("skills parent stat error = %v", err)
	}
}

func TestUninstallPreservesUnrelatedSkills(t *testing.T) {
	manager := newManager(t, t.TempDir(), nil)
	installed, err := manager.Install(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	)
	if err != nil {
		t.Fatal(err)
	}
	otherSkill := filepath.Join(
		filepath.Dir(installed.Targets[0].SkillPath),
		"other-skill",
		"SKILL.md",
	)
	writeTestFile(t, otherSkill, "other\n")

	if _, err := manager.Uninstall(
		context.Background(),
		agentintegration.Options{Target: agentintegration.TargetCodex},
	); err != nil {
		t.Fatal(err)
	}

	if got := readTestFile(t, otherSkill); got != "other\n" {
		t.Fatalf("other skill = %q", got)
	}
}

func newManager(
	t *testing.T,
	home string,
	environ map[string]string,
) *agentintegration.Manager {
	t.Helper()
	return newManagerWithDependencies(t, home, environ, nil)
}

func newManagerWithDependencies(
	t *testing.T,
	home string,
	environ map[string]string,
	writeFile func(string, []byte, fs.FileMode) error,
) *agentintegration.Manager {
	t.Helper()
	return agentintegration.NewManager(agentintegration.Dependencies{
		HomeDir: func() (string, error) {
			return home, nil
		},
		LookupEnv: func(name string) (string, bool) {
			value, ok := environ[name]
			return value, ok
		},
		Assets: fstest.MapFS{
			"wktbox-isolated-development/SKILL.md": {
				Data: []byte("---\nname: wktbox-isolated-development\n" +
					"description: test\n---\n\n# Test\n"),
				Mode: 0o644,
			},
			"wktbox-isolated-development/agents/openai.yaml": {
				Data: []byte("interface:\n  display_name: \"Wktbox\"\n"),
				Mode: 0o644,
			},
		},
		Version:   "0.1.0",
		WriteFile: writeFile,
	})
}

func statusFor(
	t *testing.T,
	manager *agentintegration.Manager,
	target agentintegration.Target,
) agentintegration.TargetStatus {
	t.Helper()
	report, err := manager.Status(
		context.Background(),
		agentintegration.Options{Target: target},
	)
	if err != nil {
		t.Fatal(err)
	}
	return report.Targets[0]
}

func writeCanonicalSkill(t *testing.T, root string) {
	t.Helper()
	writeTestFile(
		t,
		filepath.Join(root, "SKILL.md"),
		"---\nname: wktbox-isolated-development\n"+
			"description: test\n---\n\n# Test\n",
	)
	writeTestFile(
		t,
		filepath.Join(root, "agents", "openai.yaml"),
		"interface:\n  display_name: \"Wktbox\"\n",
	)
}

func writeTestFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertPath(t *testing.T, got string, want string) {
	t.Helper()
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func assertMode(t *testing.T, path string, want fs.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %#o, want %#o", path, got, want)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
