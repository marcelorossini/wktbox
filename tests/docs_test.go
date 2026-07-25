package tests_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var publicDocs = []string{
	"README.md",
	"README.pt-BR.md",
	"CONTRIBUTING.md",
	"CHANGELOG.md",
	"docs/installation.md",
	"docs/quickstart.md",
	"docs/worktrees.md",
	"docs/agents.md",
	"docs/connections.md",
	"docs/port-forwarding.md",
	"docs/configuration.md",
	"docs/release-verification.md",
	"docs/security.md",
	"docs/troubleshooting.md",
	"docs/windows.md",
	"docs/pt-BR/configuration.md",
	"docs/pt-BR/connections.md",
	"docs/pt-BR/port-forwarding.md",
	"docs/pt-BR/security.md",
	"docs/pt-BR/windows.md",
}

func TestDocsExplainBidirectionalPortForwarding(t *testing.T) {
	english := readProjectFile(t, "docs/port-forwarding.md")
	portuguese := readProjectFile(t, "docs/pt-BR/port-forwarding.md")
	for _, expected := range []string{
		"wktbox port import",
		"wktbox port publish",
		"wktbox port list",
		"wktbox port remove",
		"localhost:1234",
		"TCP only",
		"127.0.0.1",
		"multiple services",
		"entire batch",
		"stopped",
		"restart",
		"--json",
	} {
		if !strings.Contains(english, expected) {
			t.Errorf("port-forwarding docs missing %q", expected)
		}
	}
	for _, expected := range []string{
		"wktbox port import",
		"wktbox port publish",
		"localhost:1234",
		"vários serviços",
		"lote inteiro",
		"container é parado",
	} {
		if !strings.Contains(portuguese, expected) {
			t.Errorf("Portuguese port-forwarding docs missing %q", expected)
		}
	}
	security := readProjectFile(t, "docs/security.md")
	for _, expected := range []string{
		"256-bit",
		"host Docker socket",
		"loopback",
	} {
		if !strings.Contains(security, expected) {
			t.Errorf("security docs missing forwarding invariant %q", expected)
		}
	}
}

func TestDocsExplainCrossBoxConnectionsAndSecurityBoundary(t *testing.T) {
	english := readProjectFile(t, "docs/connections.md")
	portuguese := readProjectFile(t, "docs/pt-BR/connections.md")
	security := readProjectFile(t, "docs/security.md")
	for _, expected := range []string{
		"wktbox connect",
		"wktbox connections",
		"wktbox disconnect",
		".wktbox",
		"published port",
		"ready",
		"restart",
	} {
		if !strings.Contains(strings.ToLower(english), strings.ToLower(expected)) {
			t.Errorf("connection docs missing %q", expected)
		}
	}
	for _, expected := range []string{
		"wktbox connect",
		"wktbox connections",
		"wktbox disconnect",
		".wktbox",
	} {
		if !strings.Contains(portuguese, expected) {
			t.Errorf("Portuguese connection docs missing %q", expected)
		}
	}
	if !strings.Contains(security, "Connecting boxes") ||
		!strings.Contains(security, "TLS") {
		t.Error("security docs do not explain the cross-box trust boundary")
	}
}

func TestDocsExplainBrowserCDPDiscoveryAndSecurity(t *testing.T) {
	required := map[string][]string{
		"README.md": {
			"Browser CDP",
			"browserCdp",
			"23005",
		},
		"docs/configuration.md": {
			"9222",
			"offset `+5`",
			"always running",
		},
		"docs/security.md": {
			"CDP",
			"cookies",
			"127.0.0.1",
		},
		"README.pt-BR.md": {
			"CDP do navegador",
			"browserCdp",
		},
		"docs/pt-BR/configuration.md": {
			"9222",
			"offset `+5`",
		},
		"docs/pt-BR/security.md": {
			"CDP",
			"cookies",
			"127.0.0.1",
		},
	}
	for path, phrases := range required {
		body := strings.Join(strings.Fields(readProjectFile(t, path)), " ")
		for _, phrase := range phrases {
			if !strings.Contains(body, phrase) {
				t.Errorf("%s missing %q", path, phrase)
			}
		}
	}
}

func TestDocsBrandingPackageIsTrackedAndRenderedWithoutSourceArchive(t *testing.T) {
	root := projectRoot(t)
	readme := readProjectFile(t, "README.md")
	for _, logo := range []string{
		"docs/assets/branding/wktbox-logo-light.svg",
		"docs/assets/branding/wktbox-logo-dark.svg",
	} {
		if !strings.Contains(readme, logo) {
			t.Errorf("README missing branding logo %q", logo)
		}
	}

	for _, name := range []string{
		"README.md",
		"wktbox-icon.svg",
		"wktbox-icon-monochrome.svg",
		"wktbox-icon-128.png",
		"wktbox-icon-512.png",
		"wktbox-icon-1024.png",
		"wktbox-logo-compact.svg",
		"wktbox-logo-compact.png",
		"wktbox-logo-light.svg",
		"wktbox-logo-light.png",
		"wktbox-logo-dark.svg",
		"wktbox-logo-dark.png",
	} {
		path := filepath.Join(root, "docs", "assets", "branding", name)
		if info, err := os.Stat(path); err != nil {
			t.Errorf("branding asset %s is missing: %v", name, err)
		} else if !info.Mode().IsRegular() {
			t.Errorf("branding asset %s is not a regular file", name)
		}
	}

	archive := filepath.Join(root, "WKTBox-logo-package (1).zip")
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Errorf("source branding archive still exists: %v", err)
	}
}

func TestDocsChangelogDescribesV030Release(t *testing.T) {
	changelog := readProjectFile(t, "CHANGELOG.md")
	for _, expected := range []string{
		"## [0.3.0] - 2026-07-24",
		"wktbox port import",
		"wktbox port publish",
		"graphical Chromium",
		"browserCdp",
		"branding",
		"[Unreleased]: https://github.com/marcelorossini/wktbox/compare/v0.3.0...HEAD",
		"[0.3.0]: https://github.com/marcelorossini/wktbox/compare/v0.2.0...v0.3.0",
	} {
		if !strings.Contains(changelog, expected) {
			t.Errorf("CHANGELOG.md missing v0.3.0 release detail %q", expected)
		}
	}
}

func TestDocsUseEnglishAsCanonicalLanguage(t *testing.T) {
	readme := readProjectFile(t, "README.md")
	for _, expected := range []string{
		"isolated Docker development",
		"Current checkout",
		"Documentation",
		"Português",
	} {
		if !strings.Contains(readme, expected) {
			t.Errorf("canonical README missing %q", expected)
		}
	}
	for _, path := range []string{
		"docs/configuration.md",
		"docs/security.md",
		"docs/windows.md",
	} {
		body := readProjectFile(t, path)
		if !strings.Contains(body, "# ") ||
			strings.Contains(body, "# Configuração") ||
			strings.Contains(body, "# Modelo de segurança") {
			t.Errorf("%s is not canonical English documentation", path)
		}
	}
}

func TestDocsLocalLinksResolve(t *testing.T) {
	linkPattern := regexp.MustCompile(`!?\[[^]]*]\(([^)]+)\)`)
	root := projectRoot(t)
	for _, relative := range publicDocs {
		body := readProjectFile(t, relative)
		for _, match := range linkPattern.FindAllStringSubmatch(body, -1) {
			target := strings.TrimSpace(match[1])
			target = strings.Trim(target, "<>")
			if target == "" ||
				strings.HasPrefix(target, "#") ||
				strings.Contains(target, "://") ||
				strings.HasPrefix(target, "mailto:") {
				continue
			}
			target, _, _ = strings.Cut(target, "#")
			resolved := filepath.Clean(filepath.Join(
				root,
				filepath.Dir(relative),
				filepath.FromSlash(target),
			))
			if _, err := os.Stat(resolved); err != nil {
				t.Errorf(
					"%s links to missing %q (%s): %v",
					relative,
					target,
					resolved,
					err,
				)
			}
		}
	}
}

func TestDocsPresentCurrentCheckoutBeforeOptionalWorktrees(t *testing.T) {
	readme := readProjectFile(t, "README.md")
	current := strings.Index(readme, "## Current checkout")
	worktree := strings.Index(readme, "## Optional linked worktree")
	if current < 0 || worktree < 0 || current >= worktree {
		t.Fatalf(
			"README order: current checkout=%d, optional worktree=%d",
			current,
			worktree,
		)
	}
	combined := strings.ToLower(readme + readProjectFile(t, "docs/quickstart.md"))
	for _, forbidden := range []string{
		"a worktree is required",
		"worktree is mandatory",
		"must create a worktree",
	} {
		if strings.Contains(combined, forbidden) {
			t.Errorf("documentation makes worktrees mandatory: %q", forbidden)
		}
	}
	if !strings.Contains(combined, "worktree is optional") {
		t.Error("documentation does not explicitly make linked worktrees optional")
	}
}

func TestDocsAcceptAnyExistingDirectoryWithoutGit(t *testing.T) {
	required := map[string][]string{
		"README.md": {
			"any existing directory",
			"Git is optional",
			"`--path` selects that exact directory",
		},
		"docs/quickstart.md": {
			"does not need to be a Git repository",
			"wktbox --path /absolute/project/path doctor",
		},
		"docs/worktrees.md": {
			"path-based identity",
			"Git-root identities remain compatible",
		},
		"docs/agents.md": {
			"Never run `git init` only to satisfy Wktbox",
		},
		"docs/troubleshooting.md": {
			"not a Git repository",
			"valid Wktbox workspace",
		},
		"docs/configuration.md": {
			"`--path` selects the exact workspace directory",
		},
		"docs/pt-BR/configuration.md": {
			"`--path` seleciona exatamente o diretório do workspace",
		},
	}
	for path, phrases := range required {
		body := strings.Join(strings.Fields(readProjectFile(t, path)), " ")
		for _, phrase := range phrases {
			if !strings.Contains(body, phrase) {
				t.Errorf("%s missing %q", path, phrase)
			}
		}
	}
	if strings.Contains(readProjectFile(t, "README.md"), "Requirements: Git,") {
		t.Error("README still presents Git as a global requirement")
	}
}

func TestDocsCoverInstallUpdatePathAndRemovalOnUnixAndWindows(t *testing.T) {
	body := readProjectFile(t, "docs/installation.md")
	for _, expected := range []string{
		"curl -fsSL",
		"install.sh",
		"irm https://github.com/marcelorossini/wktbox/releases/latest/download/install.ps1",
		"iex",
		"--version 0.1.0",
		"-Version 0.1.0",
		"PATH",
		"Update",
		"Uninstall",
		"rm",
		"Remove-Item",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("installation docs missing %q", expected)
		}
	}
}

func TestDocsExplainChecksumsAttestationsAndReleaseTargets(t *testing.T) {
	body := readProjectFile(t, "docs/release-verification.md")
	for _, expected := range []string{
		"checksums.txt",
		"sha256sum",
		"Get-FileHash",
		"gh attestation verify",
		"linux/amd64",
		"linux/arm64",
		"darwin/amd64",
		"darwin/arm64",
		"windows/amd64",
		"windows/arm64",
		"OCI",
		"anonymous",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("release verification docs missing %q", expected)
		}
	}
}

func TestDocsKeepPortugueseEntryPointAndDetailsReachable(t *testing.T) {
	for _, path := range []string{
		"README.pt-BR.md",
		"docs/pt-BR/configuration.md",
		"docs/pt-BR/security.md",
		"docs/pt-BR/windows.md",
	} {
		body := readProjectFile(t, path)
		if strings.TrimSpace(body) == "" {
			t.Errorf("%s is empty", path)
		}
	}
	readme := readProjectFile(t, "README.md")
	if !strings.Contains(readme, "(README.pt-BR.md)") {
		t.Error("canonical README does not link to Portuguese entry point")
	}
}

func TestDocsExplainTrustedCodeSecurityBoundary(t *testing.T) {
	body := readProjectFile(t, "docs/security.md")
	body = strings.Join(strings.Fields(body), " ")
	for _, expected := range []string{
		"operational isolation",
		"trusted development code",
		"privileged Docker-in-Docker",
		"not a security boundary",
		"writable",
		"/var/run/docker.sock",
		"127.0.0.1",
		"`::1`",
		"Unix socket",
		"virtual machine",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("security docs missing %q", expected)
		}
	}
	for _, path := range []string{
		"README.md",
		"docs/security.md",
		"docs/windows.md",
	} {
		content := readProjectFile(t, path)
		for _, forbidden := range []string{
			"VM-equivalent isolation",
			"safe for untrusted code",
		} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("%s contains forbidden claim %q", path, forbidden)
			}
		}
	}
}

func TestDocsCoverAgentIntegrationLifecycleAndConflicts(t *testing.T) {
	body := readProjectFile(t, "docs/agents.md")
	for _, expected := range []string{
		"agents install",
		"agents status",
		"agents uninstall",
		"Update",
		"Codex",
		"Claude",
		"--target all",
		"--dry-run",
		"conflict",
		"--force",
		"managed instruction block",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("agent docs missing %q", expected)
		}
	}
}

func TestDocsListCLIContractAndAutomaticLoopback(t *testing.T) {
	readme := readProjectFile(t, "README.md")
	for _, command := range []string{
		"up", "run", "exec", "compose", "shell", "open", "list",
		"status", "logs", "stop", "restart", "destroy", "doctor",
		"prune", "agents", "connect", "connections", "disconnect",
	} {
		if !strings.Contains(readme, "`wktbox "+command) {
			t.Errorf("README does not document %q", command)
		}
	}
	for _, expected := range []string{
		"localhost:5173",
		"published port",
		"`wktbox status`",
		"conflict",
		"UDP",
		"--json",
	} {
		if !strings.Contains(readme, expected) {
			t.Errorf("README missing runtime concept %q", expected)
		}
	}
	configuration := readProjectFile(t, "docs/configuration.md")
	for _, expected := range []string{
		"defaults",
		".wktbox.yml",
		".wktbox.local.yml",
		"WKTBOX_",
		"flags",
		"`ports:`",
		"NetworkSettings.Ports",
		"61000",
		"61001",
		"61002",
		"gateway",
	} {
		if !strings.Contains(configuration, expected) {
			t.Errorf("configuration docs missing %q", expected)
		}
	}
	windows := readProjectFile(t, "docs/windows.md")
	for _, expected := range []string{
		"network_mode: service:webtop",
		"Linux containers",
	} {
		if !strings.Contains(windows, expected) {
			t.Errorf("Windows docs missing platform detail %q", expected)
		}
	}
}

func TestBuildScriptDeclaresEveryReleaseTarget(t *testing.T) {
	body := readProjectFile(t, "scripts/build.sh") +
		readProjectFile(t, "scripts/release_lib.sh")
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
	data, err := os.ReadFile(filepath.Join(projectRoot(t), relative))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func projectRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate documentation test")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), ".."))
}
