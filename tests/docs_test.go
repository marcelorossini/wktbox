package tests_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSecurityDocumentationDoesNotPromiseUntrustedCodeSafety(t *testing.T) {
	for _, path := range []string{"README.md", "docs/security.md", "docs/windows.md"} {
		body := readProjectFile(t, path)
		for _, forbidden := range []string{
			"VM-equivalent isolation",
			"execução segura de código não confiável",
		} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s contains forbidden claim %q", path, forbidden)
			}
		}
	}
}

func TestREADMEListsEveryMVPCommandAndStructuredOutput(t *testing.T) {
	body := readProjectFile(t, "README.md")
	for _, command := range []string{
		"up", "run", "exec", "compose", "shell", "open", "list",
		"status", "logs", "stop", "restart", "destroy", "doctor",
	} {
		if !strings.Contains(body, "`wktbox "+command) {
			t.Errorf("README does not document %q", command)
		}
	}
	for _, expected := range []string{"--json", "duas worktrees", "isolamento operacional"} {
		if !strings.Contains(body, expected) {
			t.Errorf("README missing %q", expected)
		}
	}
}

func TestConfigurationDocumentationCoversPrecedenceAndEverySection(t *testing.T) {
	body := readProjectFile(t, "docs/configuration.md")
	for _, expected := range []string{
		"defaults",
		".wktbox.yml",
		".wktbox.local.yml",
		"WKTBOX_",
		"flags",
		"workspace",
		"environment",
		"runtime",
		"webtop",
		"git",
		"resources",
		"gateway",
		"commands",
		"lifecycle",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("configuration docs missing %q", expected)
		}
	}
}

func TestBuildScriptDeclaresEveryReleaseTarget(t *testing.T) {
	body := readProjectFile(t, "scripts/build.sh")
	for _, target := range []string{
		"windows/amd64",
		"windows/arm64",
		"linux/amd64",
		"linux/arm64",
		"darwin/amd64",
		"darwin/arm64",
	} {
		if !strings.Contains(body, target) {
			t.Errorf("build script missing %q", target)
		}
	}
}

func readProjectFile(t *testing.T, relative string) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate documentation test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), ".."))
	data, err := os.ReadFile(filepath.Join(root, relative))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
