package sandbox_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"

	"wktbox/internal/config"
	"wktbox/internal/environment"
	"wktbox/internal/ports"
	"wktbox/internal/sandbox"
)

type composeDocument struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Labels  map[string]string `yaml:"labels"`
	Volumes []any             `yaml:"volumes"`
}

func TestRenderMountsWorkspaceInDockerAndWebtop(t *testing.T) {
	files, err := sandbox.Render(t.TempDir(), testSpec())
	if err != nil {
		t.Fatal(err)
	}
	document := readCompose(t, files.ComposePath)

	for _, serviceName := range []string{"docker", "webtop"} {
		service := document.Services[serviceName]
		assertBind(t, service.Volumes, "${WORKTREE_PATH}", "/workspace", false)
		if service.Labels["io.wktbox.managed"] != "true" {
			t.Fatalf("%s managed label = %q", serviceName, service.Labels["io.wktbox.managed"])
		}
		if service.Labels["io.wktbox.box-id"] != "${WKTBOX_ID}" {
			t.Fatalf("%s box label = %q", serviceName, service.Labels["io.wktbox.box-id"])
		}
	}

	body := readFile(t, files.ComposePath)
	if strings.Contains(body, "/var/run/docker.sock") {
		t.Fatal("host Docker socket leaked into sandbox")
	}
	if strings.Contains(body, "PROJECT_ENV_PATH") {
		t.Fatal("base Compose unexpectedly requires a project env")
	}
}

func TestRenderProjectEnvOverrideContainsPathNotContent(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "external env", "project.env")
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("TOKEN=do-not-copy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec := testSpec()
	spec.ProjectEnv = environment.ProjectEnv{
		Source:   source,
		Target:   "/workspace/.env",
		Mount:    true,
		ReadOnly: true,
	}

	files, err := sandbox.Render(filepath.Join(root, "box"), spec)
	if err != nil {
		t.Fatal(err)
	}
	if files.ProjectEnvOverridePath == "" {
		t.Fatal("project env override path is empty")
	}
	document := readCompose(t, files.ProjectEnvOverridePath)
	for _, serviceName := range []string{"docker", "webtop"} {
		assertBind(
			t,
			document.Services[serviceName].Volumes,
			source,
			"/workspace/.env",
			true,
		)
	}

	override := readFile(t, files.ProjectEnvOverridePath)
	sandboxEnv := readFile(t, files.SandboxEnvPath)
	for name, body := range map[string]string{
		"override":    override,
		"sandbox env": sandboxEnv,
	} {
		if strings.Contains(body, "do-not-copy") {
			t.Fatalf("%s leaked project env content", name)
		}
	}
	if strings.Contains(sandboxEnv, source) {
		t.Fatal("sandbox env contains project env source")
	}
}

func TestRenderWithoutProjectEnvRemovesStaleOverride(t *testing.T) {
	directory := t.TempDir()
	stale := filepath.Join(directory, "project-env.override.yml")
	if err := os.WriteFile(stale, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	files, err := sandbox.Render(directory, testSpec())
	if err != nil {
		t.Fatal(err)
	}
	if files.ProjectEnvOverridePath != "" {
		t.Fatalf("override path = %q", files.ProjectEnvOverridePath)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale override still exists: %v", err)
	}
	if got := files.ComposeFiles(); len(got) != 1 || got[0] != files.ComposePath {
		t.Fatalf("compose files = %#v", got)
	}
}

func TestRenderWritesDeterministicProtectedFiles(t *testing.T) {
	directory := t.TempDir()
	first, err := sandbox.Render(directory, testSpec())
	if err != nil {
		t.Fatal(err)
	}
	firstCompose := readFile(t, first.ComposePath)
	firstEnv := readFile(t, first.SandboxEnvPath)

	second, err := sandbox.Render(directory, testSpec())
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, second.ComposePath); got != firstCompose {
		t.Fatal("Compose output is not deterministic")
	}
	if got := readFile(t, second.SandboxEnvPath); got != firstEnv {
		t.Fatal("sandbox env output is not deterministic")
	}
	for _, path := range []string{second.ComposePath, second.SandboxEnvPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("%s permissions = %o", path, info.Mode().Perm())
		}
	}
}

func TestSandboxEnvContainsOnlyControlPlaneValues(t *testing.T) {
	files, err := sandbox.Render(t.TempDir(), testSpec())
	if err != nil {
		t.Fatal(err)
	}
	body := readFile(t, files.SandboxEnvPath)
	for _, line := range []string{
		`WKTBOX_ID="a4f8c9137d2b"`,
		`WORKTREE_PATH="/repo tree/feature"`,
		`PORT_HTTP="23000"`,
		`PORT_GATEWAY="23003"`,
		`WKTBOX_DIND_IMAGE="docker:29.5.0-dind"`,
	} {
		if !strings.Contains(body, line) {
			t.Fatalf("sandbox env missing %q:\n%s", line, body)
		}
	}
}

func testSpec() sandbox.Spec {
	return sandbox.Spec{
		ID:       "a4f8c9137d2b",
		Version:  "dev",
		Worktree: "/repo tree/feature",
		Ports:    ports.Block{Start: 23000, Size: 10},
		Config:   config.Default(),
		Timezone: "America/Sao_Paulo",
		PUID:     1000,
		PGID:     1000,
	}
}

func readCompose(t *testing.T, path string) composeDocument {
	t.Helper()
	var document composeDocument
	if err := yaml.Load([]byte(readFile(t, path)), &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func assertBind(
	t *testing.T,
	volumes []any,
	source string,
	target string,
	readOnly bool,
) {
	t.Helper()
	for _, rawVolume := range volumes {
		volume, ok := rawVolume.(map[string]any)
		if !ok {
			continue
		}
		if volume["type"] == "bind" && volume["source"] == source && volume["target"] == target {
			actualReadOnly := false
			if rawReadOnly, exists := volume["read_only"]; exists {
				var valid bool
				actualReadOnly, valid = rawReadOnly.(bool)
				if !valid {
					t.Fatalf("bind %#v has non-boolean read_only", volume)
				}
			}
			if actualReadOnly != readOnly {
				t.Fatalf("bind %#v read_only = %t", volume, actualReadOnly)
			}
			return
		}
	}
	t.Fatalf("bind %q -> %q not found in %#v", source, target, volumes)
}
