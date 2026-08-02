# Webtop Development Tools Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship NVM with Node.js LTS, Python, Visual Studio Code, DBeaver Community, and a compact set of developer utilities in every Wktbox Webtop image.

**Architecture:** The immutable Webtop image installs Ubuntu packages and the official VS Code and DBeaver repositories during its build. NVM and Node.js LTS are built once into a read-only seed, then an idempotent startup helper copies that seed into the persistent `/config/.nvm` directory only when it is absent. System shell startup loads the persistent NVM installation for the `abc` desktop user.

**Tech Stack:** Docker multi-stage builds, LinuxServer Webtop Ubuntu/XFCE, Bash/POSIX shell, NVM v0.40.4, Node.js active LTS, Ubuntu Python 3, Microsoft VS Code APT repository, DBeaver Community APT repository, Go documentation tests.

## Global Constraints

- Preserve release support for both `linux/amd64` and `linux/arm64`.
- Install NVM, Node.js LTS, Python, Visual Studio Code, and DBeaver Community in the immutable Webtop image.
- Keep NVM versions and all graphical application settings under the existing persistent `/config` volume.
- Never overwrite an existing `/config/.nvm` directory during container startup.
- Use signed official APT repositories and keyrings; do not use `apt-key` or architecture-specific `amd64` download URLs.
- Include `build-essential`, `jq`, `ripgrep`, `unzip`, and `zip`; do not add unrelated tools or editor extensions.
- Do not preconfigure DBeaver connections, credentials, or drivers.
- Run every host shell command through `rtk` and every project build/test command through Docker-backed project targets or Docker itself.

---

### Task 1: Install Python, graphical applications, and baseline utilities

**Files:**
- Create: `tests/images/development_tools_test.sh`
- Modify: `images/webtop/Dockerfile`
- Modify: `Makefile`

**Interfaces:**
- Consumes: existing `images/webtop/Dockerfile` build context and the `wktbox/webtop` Ubuntu/XFCE base.
- Produces: image commands `python`, `python3`, `pip`, `pipx`, `code`, `dbeaver`, `cc`, `make`, `jq`, `rg`, `unzip`, and `zip`; Make target `images-test` runs the new contract test.

- [ ] **Step 1: Write the failing image contract test**

Create `tests/images/development_tools_test.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
image="${WKTBOX_DEVTOOLS_TEST_IMAGE:-wktbox/webtop:development-tools-test}"

docker build \
  --file "$project_root/images/webtop/Dockerfile" \
  --tag "$image" \
  "$project_root"

docker run --rm --entrypoint bash "$image" -lc '
  set -euo pipefail

  python --version
  python3 --version
  pip --version
  pipx --version
  code --version --no-sandbox --user-data-dir=/tmp/wktbox-code-test
  command -v dbeaver

  for command_name in cc make jq rg unzip zip; do
    command -v "$command_name"
  done

  dpkg-query --show \
    python3 python3-pip python3-venv python-is-python3 pipx \
    code dbeaver-ce build-essential jq ripgrep unzip zip
'

printf 'Webtop development package tests passed\n'
```

Add it to the end of the `images-test` recipe in `Makefile`:

```make
images-test:
	bash tests/images/chromium_test.sh
	bash tests/images/labels_test.sh
	bash tests/images/development_tools_test.sh
```

- [ ] **Step 2: Run the test and verify it fails**

Run:

```bash
rtk bash tests/images/development_tools_test.sh
```

Expected: FAIL because at least `python`, `pipx`, `code`, or `dbeaver` is not available in the current Webtop image.

- [ ] **Step 3: Register signed repositories and install the packages**

In `images/webtop/Dockerfile`, replace the final-stage package `RUN` block with:

```dockerfile
RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      bash \
      build-essential \
      ca-certificates \
      curl \
      git \
      gnupg \
      jq \
      openssh-client \
      pipx \
      python-is-python3 \
      python3 \
      python3-pip \
      python3-venv \
      ripgrep \
      socat \
      unzip \
      zip \
    && install -d -m 0755 /etc/apt/keyrings \
    && curl -fsSL https://packages.microsoft.com/keys/microsoft.asc \
      | gpg --dearmor --batch --yes -o /etc/apt/keyrings/microsoft.gpg \
    && chmod a+r /etc/apt/keyrings/microsoft.gpg \
    && printf '%s\n' \
      'Types: deb' \
      'URIs: https://packages.microsoft.com/repos/code' \
      'Suites: stable' \
      'Components: main' \
      'Architectures: amd64 arm64' \
      'Signed-By: /etc/apt/keyrings/microsoft.gpg' \
      > /etc/apt/sources.list.d/vscode.sources \
    && curl -fsSL https://dbeaver.io/debs/dbeaver.gpg.key \
      | gpg --dearmor --batch --yes -o /etc/apt/keyrings/dbeaver.gpg \
    && chmod a+r /etc/apt/keyrings/dbeaver.gpg \
    && printf '%s\n' \
      'deb [arch=amd64,arm64 signed-by=/etc/apt/keyrings/dbeaver.gpg] https://dbeaver.io/debs/dbeaver-ce /' \
      > /etc/apt/sources.list.d/dbeaver.list \
    && apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      code \
      dbeaver-ce \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*
```

Extend the existing Dockerfile validation `RUN` command with:

```dockerfile
RUN python --version \
    && python3 --version \
    && pip --version \
    && pipx --version \
    && code --version --no-sandbox --user-data-dir=/tmp/wktbox-code-build-test \
    && command -v dbeaver \
    && command -v cc \
    && command -v jq \
    && command -v rg
```

- [ ] **Step 4: Run the focused image test and verify it passes**

Run:

```bash
rtk bash tests/images/development_tools_test.sh
```

Expected: PASS and final line `Webtop development package tests passed`.

- [ ] **Step 5: Verify the existing image contracts still pass**

Run:

```bash
rtk bash tests/images/chromium_test.sh
rtk bash tests/images/labels_test.sh
```

Expected: both scripts exit 0 and print their existing success messages.

- [ ] **Step 6: Commit the package layer**

```bash
rtk git add images/webtop/Dockerfile tests/images/development_tools_test.sh Makefile
rtk git commit -m "feat: adicionar ferramentas de desenvolvimento ao webtop"
```

---

### Task 2: Persist NVM and Node.js LTS for the desktop user

**Files:**
- Create: `images/webtop/init-nvm.sh`
- Create: `images/webtop/wktbox-nvm.sh`
- Modify: `images/webtop/Dockerfile`
- Modify: `images/webtop/entrypoint.sh`
- Modify: `tests/images/development_tools_test.sh`

**Interfaces:**
- Consumes: `PUID`, `PGID`, `/config`, the LinuxServer `abc` user, and the image test created in Task 1.
- Produces: immutable seed `/usr/local/share/nvm`, persistent `NVM_DIR=/config/.nvm`, executable `/usr/local/bin/wktbox-init-nvm`, profile `/etc/profile.d/wktbox-nvm.sh`, and default Node.js LTS commands.

- [ ] **Step 1: Extend the image test with the missing NVM contract**

Add these variables and cleanup immediately after `image=...` in
`tests/images/development_tools_test.sh`:

```bash
test_root="$(mktemp -d)"
config_volume="wktbox-nvm-test-$$-$RANDOM"
container="wktbox-nvm-test-$$-$RANDOM"

cleanup() {
  docker rm --force "$container" >/dev/null 2>&1 || true
  docker volume rm --force "$config_volume" >/dev/null 2>&1 || true
  rm -rf "$test_root"
}
trap cleanup EXIT

docker volume create "$config_volume" >/dev/null
```

Before the final `printf`, create a user-side checker and exercise the real
Webtop entrypoint twice:

```bash
cat >"$test_root/check-nvm.sh" <<'SH'
#!/usr/bin/env bash
set -euo pipefail

export HOME=/config
. /etc/profile.d/wktbox-nvm.sh

test "$(nvm --version)" = "0.40.4"
node --version
npm --version
npx --version
corepack --version
test -n "$(node -p 'process.release.lts')"
test -w "$NVM_DIR"
touch "$NVM_DIR/.wktbox-user-marker"
SH
chmod +x "$test_root/check-nvm.sh"

start_webtop() {
  docker run --detach \
    --name "$container" \
    --shm-size 1g \
    --env PUID=1000 \
    --env PGID=1000 \
    --volume "$config_volume:/config" \
    --volume "$test_root/check-nvm.sh:/tmp/check-nvm.sh:ro" \
    "$image" >/dev/null
}

wait_for_nvm() {
  local deadline=$((SECONDS + 30))

  while ((SECONDS < deadline)); do
    if docker exec "$container" test -s /config/.nvm/nvm.sh; then
      return 0
    fi
    if [[ "$(docker inspect --format '{{.State.Running}}' "$container")" != "true" ]]; then
      docker logs "$container" >&2
      return 1
    fi
    sleep 0.25
  done

  docker logs "$container" >&2
  return 1
}

start_webtop
wait_for_nvm
docker exec "$container" s6-setuidgid abc /tmp/check-nvm.sh
docker rm --force "$container" >/dev/null

start_webtop
wait_for_nvm
docker exec "$container" test -f /config/.nvm/.wktbox-user-marker
docker rm --force "$container" >/dev/null
```

The package-only smoke check earlier in the file remains a short-lived
`docker run --rm`. The two detached runs above specifically prove that the
default entrypoint seeds `/config` before LinuxServer `/init` and that a later
start preserves user content.

- [ ] **Step 2: Run the test and verify the NVM contract fails**

Run:

```bash
rtk bash tests/images/development_tools_test.sh
```

Expected: FAIL because `/usr/local/bin/wktbox-init-nvm` and the NVM profile do not exist.

- [ ] **Step 3: Add the idempotent NVM initializer**

Create `images/webtop/init-nvm.sh`:

```sh
#!/bin/sh
set -eu

nvm_seed="${WKTBOX_NVM_SEED:-/usr/local/share/nvm}"
nvm_dir="${NVM_DIR:-/config/.nvm}"
puid="${PUID:-1000}"
pgid="${PGID:-1000}"

if [ ! -e "$nvm_dir" ]; then
  nvm_parent="$(dirname "$nvm_dir")"
  nvm_tmp="${nvm_dir}.wktbox-tmp-$$"

  install -d -m 0755 "$nvm_parent"
  install -d -m 0755 "$nvm_tmp"
  trap 'rm -rf -- "$nvm_tmp"' EXIT HUP INT TERM
  cp -a "$nvm_seed/." "$nvm_tmp/"
  chown -R "$puid:$pgid" "$nvm_tmp"
  mv "$nvm_tmp" "$nvm_dir"
  trap - EXIT HUP INT TERM
fi

chown -R "$puid:$pgid" "$nvm_dir"
```

Create `images/webtop/wktbox-nvm.sh`:

```sh
export NVM_DIR="${NVM_DIR:-${HOME:-/config}/.nvm}"

if [ -s "$NVM_DIR/nvm.sh" ]; then
  # shellcheck source=/dev/null
  . "$NVM_DIR/nvm.sh"
fi
```

- [ ] **Step 4: Build the immutable NVM/Node seed and install shell hooks**

Add these build arguments and environment variable after the existing final
stage `ARG` declarations in `images/webtop/Dockerfile`:

```dockerfile
ARG NVM_VERSION=v0.40.4
ARG NODE_VERSION=lts/*

ENV WKTBOX_NVM_SEED=/usr/local/share/nvm
```

After package installation, add:

```dockerfile
RUN git clone --branch "$NVM_VERSION" --depth 1 \
      https://github.com/nvm-sh/nvm.git "$WKTBOX_NVM_SEED" \
    && export NVM_DIR="$WKTBOX_NVM_SEED" \
    && . "$NVM_DIR/nvm.sh" \
    && nvm install "$NODE_VERSION" \
    && nvm alias default "$NODE_VERSION" \
    && nvm cache clear \
    && rm -rf "$NVM_DIR/.git" \
    && test "$(nvm --version)" = "${NVM_VERSION#v}" \
    && node --version \
    && npm --version \
    && corepack --version

COPY --chmod=0755 images/webtop/init-nvm.sh /usr/local/bin/wktbox-init-nvm
COPY --chmod=0644 images/webtop/wktbox-nvm.sh /etc/profile.d/wktbox-nvm.sh

RUN printf '%s\n' \
      '' \
      '# Wktbox persistent NVM environment' \
      '[ -r /etc/profile.d/wktbox-nvm.sh ] && . /etc/profile.d/wktbox-nvm.sh' \
      >> /etc/bash.bashrc
```

Extend the final Dockerfile file validation with:

```dockerfile
RUN test -x /usr/local/bin/wktbox-init-nvm \
    && test -r /etc/profile.d/wktbox-nvm.sh \
    && test -s /usr/local/share/nvm/nvm.sh
```

- [ ] **Step 5: Seed NVM only for the graphical Webtop entrypoint**

Update `images/webtop/entrypoint.sh` to:

```sh
#!/bin/sh
set -eu

if [ "$#" -gt 0 ]; then
  exec "$@"
fi

/usr/local/bin/wktbox-init-nvm
exec /init
```

This preserves the fast command path used by the `interconnect`, `loopback`,
and `portbridge` services, which share the image but do not mount `/config`.

- [ ] **Step 6: Run the focused test and verify package plus persistence behavior**

Run:

```bash
rtk bash tests/images/development_tools_test.sh
```

Expected: PASS, a non-empty Node.js LTS codename, and final line `Webtop development package tests passed`. The second container must find `.wktbox-user-marker`.

- [ ] **Step 7: Commit NVM persistence**

```bash
rtk git add images/webtop/Dockerfile images/webtop/entrypoint.sh \
  images/webtop/init-nvm.sh images/webtop/wktbox-nvm.sh \
  tests/images/development_tools_test.sh
rtk git commit -m "feat: persistir nvm e node lts no webtop"
```

---

### Task 3: Document the included environment in both public READMEs

**Files:**
- Modify: `tests/docs_test.go`
- Modify: `README.md`
- Modify: `README.pt-BR.md`

**Interfaces:**
- Consumes: the tool and persistence contract delivered by Tasks 1 and 2.
- Produces: searchable English and Portuguese documentation of the bundled environment and its update/persistence behavior.

- [ ] **Step 1: Write the failing documentation contract**

Add this test to `tests/docs_test.go`:

```go
func TestDocsExplainWebtopDevelopmentEnvironment(t *testing.T) {
	required := map[string][]string{
		"README.md": {
			"Webtop development environment",
			"NVM",
			"Node.js LTS",
			"Python",
			"Visual Studio Code",
			"DBeaver Community",
			"/config/.nvm",
		},
		"README.pt-BR.md": {
			"Ambiente de desenvolvimento do Webtop",
			"NVM",
			"Node.js LTS",
			"Python",
			"Visual Studio Code",
			"DBeaver Community",
			"/config/.nvm",
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
```

- [ ] **Step 2: Run the focused test and verify it fails**

Run:

```bash
rtk docker run --rm --user "$(id -u):$(id -g)" \
  -e GOCACHE=/tmp/go-build -e GOMODCACHE=/tmp/go-mod \
  -v "$PWD:/src" -w /src golang:1.26.5 \
  go test ./tests -run WebtopDevelopmentEnvironment -count=1
```

Expected: FAIL with missing phrases from both README files.

- [ ] **Step 3: Add the English environment documentation**

Insert this section in `README.md` after `Read the complete [current-checkout quickstart](docs/quickstart.md).`:

```markdown
## Webtop development environment

The Webtop image includes NVM with Node.js LTS, Python with `pip`, `venv`, and
`pipx`, Visual Studio Code, DBeaver Community, Docker CLI with Compose and
Buildx, Git, `build-essential`, `jq`, `ripgrep`, `zip`, and `unzip`.

Editor settings, DBeaver data, and additional Node.js versions persist in the
existing `/config` volume. NVM stores its versions in `/config/.nvm`. Tools
provided by the image are updated by pulling a newer Wktbox image, not by an
automatic startup update.
```

- [ ] **Step 4: Add the Portuguese environment documentation**

Insert this section in `README.pt-BR.md` after the quick-start explanation:

```markdown
## Ambiente de desenvolvimento do Webtop

A imagem do Webtop inclui NVM com Node.js LTS, Python com `pip`, `venv` e
`pipx`, Visual Studio Code, DBeaver Community, Docker CLI com Compose e Buildx,
Git, `build-essential`, `jq`, `ripgrep`, `zip` e `unzip`.

Configurações dos editores, dados do DBeaver e versões adicionais do Node.js
persistem no volume `/config` existente. O NVM armazena suas versões em
`/config/.nvm`. As ferramentas fornecidas pela imagem são atualizadas ao usar
uma imagem mais nova do Wktbox, nunca automaticamente durante a inicialização.
```

- [ ] **Step 5: Run the focused and complete documentation tests**

Run:

```bash
rtk docker run --rm --user "$(id -u):$(id -g)" \
  -e GOCACHE=/tmp/go-build -e GOMODCACHE=/tmp/go-mod \
  -v "$PWD:/src" -w /src golang:1.26.5 \
  go test ./tests -run WebtopDevelopmentEnvironment -count=1
rtk make docs-check
```

Expected: both commands exit 0.

- [ ] **Step 6: Commit the documentation**

```bash
rtk git add README.md README.pt-BR.md tests/docs_test.go
rtk git commit -m "docs: descrever ambiente de desenvolvimento do webtop"
```

---

### Task 4: Verify the complete requirement set and both release architectures

**Files:**
- Verify only; modify the responsible task's files if a check exposes a defect.

**Interfaces:**
- Consumes: all implementation and documentation delivered by Tasks 1–3.
- Produces: fresh evidence for every explicit requirement and a clean handoff.

- [ ] **Step 1: Run formatting and diff checks**

```bash
rtk git diff --check HEAD~3..HEAD
rtk docker run --rm --user "$(id -u):$(id -g)" \
  -v "$PWD:/src" -w /src golang:1.26.5 \
  sh -c 'test -z "$(gofmt -l .)"'
```

Expected: both commands exit 0 with no file names from `gofmt`.

- [ ] **Step 2: Run all image tests from a clean build path**

```bash
rtk make images-test
```

Expected: Chromium, OCI metadata, and development-tool tests all pass.

- [ ] **Step 3: Build both release architectures**

```bash
rtk docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --provenance=false \
  --file images/webtop/Dockerfile \
  --output type=cacheonly \
  .
```

Expected: exit 0 after both platform builds install VS Code, DBeaver, NVM, Node.js, and Python successfully.

- [ ] **Step 4: Run the complete project gates**

```bash
rtk make test
rtk make test-race
rtk make vet
rtk make docs-check
```

Expected: all Go tests, race tests, vet, and documentation contracts exit 0.

- [ ] **Step 5: Audit every requirement against authoritative evidence**

Inspect the final Dockerfile, scripts, tests, READMEs, and command output. Record
the following result in the handoff only when each item is proven:

```text
NVM 0.40.4: development_tools_test.sh runtime output
Node.js LTS/default npm/npx/corepack: development_tools_test.sh runtime output
Python/pip/venv/pipx: dpkg-query and runtime output
VS Code: official APT package and code --version output
DBeaver Community: official APT package and command discovery
Suggested utilities: dpkg-query and command discovery
NVM persistence/non-overwrite: two-container named-volume test
amd64 and arm64: successful buildx output for both platforms
Documentation: focused contract plus docs-check
No regressions: image tests, go test, race, vet
```

- [ ] **Step 6: Confirm repository state**

```bash
rtk git status --short
rtk git log -5 --oneline --decorate
```

Expected: no uncommitted files and commits for the design, implementation
plan, package layer, NVM persistence, and documentation.
