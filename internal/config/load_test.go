package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wktbox/internal/config"
)

func TestDefaultMatchesMVPDecisions(t *testing.T) {
	got := config.Default()
	if got.Version != 1 {
		t.Fatalf("version = %d", got.Version)
	}
	if got.Workspace.Target != "/workspace" {
		t.Fatalf("workspace target = %q", got.Workspace.Target)
	}
	if got.Environment.Target != "/workspace/.env" || !got.Environment.ReadOnly {
		t.Fatalf("environment = %#v", got.Environment)
	}
	if got.Runtime.DindImage != "docker:29.5.0-dind" {
		t.Fatalf("dind image = %q", got.Runtime.DindImage)
	}
	if got.Git.Mode != config.GitHost {
		t.Fatalf("git mode = %q", got.Git.Mode)
	}
}

func TestLoadPrecedence(t *testing.T) {
	worktree := t.TempDir()
	writeFile(t, filepath.Join(worktree, ".wktbox.yml"), `
version: 1
environment:
  file: from-project.env
  target: /workspace/.env.project
runtime:
  dindImage: docker:project-dind
`)
	writeFile(t, filepath.Join(worktree, ".wktbox.local.yml"), `
version: 1
environment:
  file: from-local.env
runtime:
  dindImage: docker:local-dind
`)

	got, err := config.Load(worktree, map[string]string{
		"WKTBOX_ENV_FILE":   "from-env.env",
		"WKTBOX_ENV_TARGET": "/run/wktbox/from-env",
	}, config.Overrides{
		EnvFile: "from-flag.env",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Environment.File != "from-flag.env" {
		t.Fatalf("environment file = %q", got.Environment.File)
	}
	if got.Environment.Target != "/run/wktbox/from-env" {
		t.Fatalf("environment target = %q", got.Environment.Target)
	}
	if got.Runtime.DindImage != "docker:local-dind" {
		t.Fatalf("dind image = %q", got.Runtime.DindImage)
	}
}

func TestLoadAlternativeConfigDoesNotReadProjectFiles(t *testing.T) {
	worktree := t.TempDir()
	writeFile(t, filepath.Join(worktree, ".wktbox.yml"), `
version: 1
environment:
  file: project.env
`)
	alternative := filepath.Join(t.TempDir(), "alternative.yml")
	writeFile(t, alternative, `
version: 1
environment:
  file: alternative.env
`)

	got, err := config.Load(worktree, nil, config.Overrides{ConfigPath: alternative})
	if err != nil {
		t.Fatal(err)
	}
	if got.Environment.File != "alternative.env" {
		t.Fatalf("environment file = %q", got.Environment.File)
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	worktree := t.TempDir()
	writeFile(t, filepath.Join(worktree, ".wktbox.yml"), `
version: 1
runtime:
  dockerSock: /var/run/docker.sock
`)

	_, err := config.Load(worktree, nil, config.Overrides{})
	if err == nil || !strings.Contains(err.Error(), "dockerSock") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadRejectsUnsupportedVersion(t *testing.T) {
	worktree := t.TempDir()
	writeFile(t, filepath.Join(worktree, ".wktbox.yml"), "version: 2\n")

	_, err := config.Load(worktree, nil, config.Overrides{})
	if err == nil || !strings.Contains(err.Error(), "version 2") {
		t.Fatalf("error = %v", err)
	}
}

func writeFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
