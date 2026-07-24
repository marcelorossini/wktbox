package sandbox_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"

	"wktbox/internal/config"
	"wktbox/internal/environment"
	"wktbox/internal/gitbridge"
	"wktbox/internal/ports"
	"wktbox/internal/sandbox"
)

type composeDocument struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Labels      map[string]string `yaml:"labels"`
	Environment map[string]string `yaml:"environment"`
	Volumes     []any             `yaml:"volumes"`
	Command     []string          `yaml:"command"`
	NetworkMode string            `yaml:"network_mode"`
	Profiles    []string          `yaml:"profiles"`
	Ports       []string          `yaml:"ports"`
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

func TestRenderIncludesRequiredLoopbackSidecarAndHighWebtopPorts(t *testing.T) {
	files, err := sandbox.Render(t.TempDir(), testSpec())
	if err != nil {
		t.Fatal(err)
	}
	document := readCompose(t, files.ComposePath)

	webtop := document.Services["webtop"]
	for key, want := range map[string]string{
		"CUSTOM_PORT":       "61000",
		"CUSTOM_HTTPS_PORT": "61001",
		"CUSTOM_WS_PORT":    "61002",
	} {
		if got := webtop.Environment[key]; got != want {
			t.Fatalf("webtop %s = %q; want %q", key, got, want)
		}
	}
	if !reflect.DeepEqual(webtop.Ports, []string{
		"127.0.0.1:${PORT_HTTP}:61000",
		"127.0.0.1:${PORT_HTTPS}:61001",
	}) {
		t.Fatalf("webtop ports = %#v", webtop.Ports)
	}

	loopbackService, exists := document.Services["loopback"]
	if !exists {
		t.Fatal("required loopback service is missing")
	}
	if loopbackService.NetworkMode != "service:webtop" {
		t.Fatalf("loopback network mode = %q", loopbackService.NetworkMode)
	}
	if !reflect.DeepEqual(loopbackService.Command, []string{"wktbox-loopback", "serve"}) {
		t.Fatalf("loopback command = %#v", loopbackService.Command)
	}
	if len(loopbackService.Profiles) != 0 {
		t.Fatalf("loopback unexpectedly has profiles: %#v", loopbackService.Profiles)
	}
	body := readFile(t, files.ComposePath)
	for _, required := range []string{
		"wktbox-loopback status --json",
		"docker-certs:/certs:ro",
		"condition: service_started",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("compose missing %q:\n%s", required, body)
		}
	}
	if strings.Contains(body, "${PORT_SSH}:22") {
		t.Fatal("reserved SSH offset is still published")
	}
}

func TestRenderIncludesInterconnectDNSWithoutHostSocket(t *testing.T) {
	files, err := sandbox.Render(t.TempDir(), testSpec())
	if err != nil {
		t.Fatal(err)
	}
	body := readFile(t, files.ComposePath)
	for _, want := range []string{
		`command: ["--dns=172.17.0.1"]`,
		"interconnect:",
		"network_mode: service:docker",
		`command: ["wktbox-loopback", "dns-serve"]`,
		"wktbox-loopback dns-probe",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("compose missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "/var/run/docker.sock") {
		t.Fatal("host Docker socket leaked into interconnect")
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

func TestRenderMountedGitBridgeOnlyOnWebtop(t *testing.T) {
	spec := testSpec()
	spec.GitBridge = gitbridge.Bridge{
		Mounts: []gitbridge.Mount{{
			Source: "/repo/.git",
			Target: "/wktbox/git-common",
		}},
		Environment: map[string]string{
			"GIT_WORK_TREE":  "/workspace",
			"GIT_DIR":        "/wktbox/git-common/worktrees/feature",
			"GIT_COMMON_DIR": "/wktbox/git-common",
		},
		RequiresValidation: true,
	}

	files, err := sandbox.Render(t.TempDir(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if files.ProjectEnvOverridePath == "" {
		t.Fatal("runtime override path is empty")
	}
	document := readCompose(t, files.ProjectEnvOverridePath)
	webtop := document.Services["webtop"]
	assertBind(t, webtop.Volumes, "/repo/.git", "/wktbox/git-common", false)
	if !reflect.DeepEqual(webtop.Environment, spec.GitBridge.Environment) {
		t.Fatalf("environment = %#v", webtop.Environment)
	}
	if docker := document.Services["docker"]; len(docker.Volumes) != 0 ||
		len(docker.Environment) != 0 {
		t.Fatalf("Git metadata exposed to DinD: %#v", docker)
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
		`GATEWAY_CONFIG_PATH="`,
	} {
		if !strings.Contains(body, line) {
			t.Fatalf("sandbox env missing %q:\n%s", line, body)
		}
	}
}

func TestRenderWritesGatewayRoutesAndBindsGeneratedConfig(t *testing.T) {
	spec := testSpec()
	spec.Config.Gateway.Enabled = true
	spec.Config.Gateway.Routes = map[string]config.Route{
		"frontend": {Port: 5173},
		"api":      {Port: 8000},
	}

	files, err := sandbox.Render(t.TempDir(), spec)
	if err != nil {
		t.Fatal(err)
	}

	gatewayConfig := readFile(t, files.GatewayConfigPath)
	for _, expected := range []string{
		"frontend.a4f8c9137d2b.localhost",
		"proxy_pass http://docker:5173",
		"api.a4f8c9137d2b.localhost",
		"proxy_pass http://docker:8000",
	} {
		if !strings.Contains(gatewayConfig, expected) {
			t.Fatalf("gateway config missing %q:\n%s", expected, gatewayConfig)
		}
	}

	document := readCompose(t, files.ComposePath)
	assertBind(
		t,
		document.Services["gateway"].Volumes,
		"${GATEWAY_CONFIG_PATH}",
		"/etc/nginx/conf.d/default.conf",
		true,
	)
}

func testSpec() sandbox.Spec {
	return sandbox.Spec{
		ID:       "a4f8c9137d2b",
		Version:  "dev",
		Worktree: "/repo tree/feature",
		Ports:    ports.Block{Start: 23000, Size: 10},
		Config:   config.Default("dev"),
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
