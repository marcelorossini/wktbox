package environment_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"wktbox/internal/config"
	"wktbox/internal/environment"
)

func TestResolveExternalProjectEnvAsReadOnlyMount(t *testing.T) {
	worktree := t.TempDir()
	source := filepath.Join(t.TempDir(), "project.env")
	writeEnv(t, source)

	got, err := environment.Resolve(worktree, config.Default(), source, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != source {
		t.Fatalf("source = %q", got.Source)
	}
	if got.Target != "/workspace/.env" {
		t.Fatalf("target = %q", got.Target)
	}
	if !got.Mount || !got.ReadOnly {
		t.Fatalf("project env = %#v", got)
	}
}

func TestResolveUsesExistingWorktreeEnvWithoutAdditionalMount(t *testing.T) {
	worktree := t.TempDir()
	source := filepath.Join(worktree, ".env")
	writeEnv(t, source)

	got, err := environment.Resolve(worktree, config.Default(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != source {
		t.Fatalf("source = %q", got.Source)
	}
	if got.Mount {
		t.Fatalf("existing worktree .env should not add a mount: %#v", got)
	}
}

func TestResolveHonorsConfiguredRelativeFile(t *testing.T) {
	worktree := t.TempDir()
	source := filepath.Join(worktree, "env", "e2e.env")
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatal(err)
	}
	writeEnv(t, source)
	cfg := config.Default()
	cfg.Environment.File = filepath.Join("env", "e2e.env")
	cfg.Environment.Target = "/workspace/.env.e2e"

	got, err := environment.Resolve(worktree, cfg, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != source || got.Target != "/workspace/.env.e2e" || !got.Mount {
		t.Fatalf("project env = %#v", got)
	}
}

func TestResolveRejectsTargetOutsideAllowedRoots(t *testing.T) {
	worktree := t.TempDir()
	source := filepath.Join(t.TempDir(), "project.env")
	writeEnv(t, source)

	_, err := environment.Resolve(worktree, config.Default(), source, "/etc/profile")
	if !errors.Is(err, environment.ErrInvalidTarget) {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveRejectsWritableConfiguration(t *testing.T) {
	worktree := t.TempDir()
	source := filepath.Join(t.TempDir(), "project.env")
	writeEnv(t, source)
	cfg := config.Default()
	cfg.Environment.ReadOnly = false

	_, err := environment.Resolve(worktree, cfg, source, "")
	if !errors.Is(err, environment.ErrWritableEnv) {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveRejectsMissingFile(t *testing.T) {
	worktree := t.TempDir()

	_, err := environment.Resolve(
		worktree,
		config.Default(),
		filepath.Join(t.TempDir(), "missing.env"),
		"",
	)
	if !errors.Is(err, environment.ErrInvalidSource) {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveReturnsNoProjectEnvWhenNoneExists(t *testing.T) {
	got, err := environment.Resolve(t.TempDir(), config.Default(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != (environment.ProjectEnv{}) {
		t.Fatalf("project env = %#v", got)
	}
}

func writeEnv(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("TOKEN=must-not-be-read\n"), 0o000); err != nil {
		t.Fatal(err)
	}
}
