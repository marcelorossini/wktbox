# Wktbox MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Entregar a CLI `wktbox` e o runtime DinD descritos em `wktbox-plano-tecnico.md`, comprovando que duas worktrees executam o mesmo Compose com portas internas idênticas e estado Docker isolado.

**Architecture:** Um binário Go atua como control plane e chama o Docker CLI/Compose v2 do host. Cada box recebe identidade estável, diretório de estado, bloco de portas e um projeto Compose externo com um daemon DinD TLS e um runner chamado `webtop`; o Compose do usuário é executado sem transformação dentro desse daemon.

**Tech Stack:** Go 1.26, Cobra 1.10.2, go.yaml.in/yaml/v4, gofrs/flock 0.13.0, Docker Engine/Compose v2, Docker Official Image 29.5.0.

## Global Constraints

- O executável chama-se `wktbox` e a configuração do projeto chama-se `.wktbox.yml`.
- O host inicial suportado é Windows 10/11 com Docker Desktop, containers Linux e Docker Compose v2; Linux é usado para desenvolvimento e CI.
- A worktree aparece como `/workspace` no runner e no daemon DinD.
- Nenhum serviço recebe `/var/run/docker.sock` do host.
- Cada box possui exatamente um daemon DinD e um projeto externo `wktbox-<id de 12 caracteres>`.
- A conexão runner → DinD usa TLS em `tcp://docker:2376`; a API não é publicada no host.
- O project env é montado como leitura, nunca copiado para estado ou logs e tem destino padrão `/workspace/.env`.
- Somente `/workspace/**` e `/run/wktbox/**` são destinos válidos para `--env-target`.
- Portas externas são reservadas em blocos de 10 a partir de 23000 e sempre vinculadas a `127.0.0.1`.
- `stop` preserva volumes; `destroy` remove somente os recursos e volumes da box selecionada.
- Argumentos do comando filho permanecem uma lista, sem reconstrução por shell.
- O estado local é cache; labels e inspeção do Docker prevalecem na reconciliação.
- O produto usa “isolamento operacional”, nunca promete segurança contra código hostil.
- Commits usam português e começam por `feat:` ou `fix:`.

---

### Task 1: Repositório, toolchain e spike da Fase 0

**Files:**
- Create: `.gitignore`
- Create: `go.mod`
- Create: `Makefile`
- Create: `cmd/wktbox/main.go`
- Create: `internal/version/version.go`
- Create: `internal/version/version_test.go`
- Create: `assets/sandbox.compose.yml`
- Create: `images/webtop/Dockerfile`
- Create: `tests/fixtures/compose-project/compose.yml`
- Create: `tests/fixtures/compose-project/project.env`
- Create: `tests/fixtures/compose-project/check.sh`
- Create: `tests/integration/spike.sh`

**Interfaces:**
- Produces: `version.String() string`.
- Produces: um runner com Docker CLI/Compose, `git`, `bash`, `curl` e certificados em `/certs/client`.
- Produces: `tests/integration/spike.sh`, que cria duas instâncias externas, executa o mesmo Compose em ambas e destrói somente os recursos que criou.

- [ ] **Step 1: inicializar o repositório e escrever o teste de versão**

```go
package version_test

import (
	"testing"

	"wktbox/internal/version"
)

func TestStringDefaultsToDevelopment(t *testing.T) {
	if got := version.String(); got != "dev" {
		t.Fatalf("String() = %q, want dev", got)
	}
}
```

- [ ] **Step 2: executar o teste em Go 1.26 e confirmar RED**

Run:

```bash
docker run --rm -v "$PWD:/src" -w /src golang:1.26.5 go test ./internal/version
```

Expected: FAIL porque `wktbox/internal/version` ainda não existe.

- [ ] **Step 3: implementar a versão mínima e o entrypoint**

```go
package version

var value = "dev"

func String() string { return value }
```

```go
package main

import (
	"fmt"

	"wktbox/internal/version"
)

func main() {
	fmt.Printf("wktbox %s\n", version.String())
}
```

- [ ] **Step 4: confirmar GREEN e criar o Compose manual do spike**

Run:

```bash
docker run --rm -v "$PWD:/src" -w /src golang:1.26.5 go test ./internal/version
docker compose -f assets/sandbox.compose.yml config
```

Expected: PASS e configuração Compose válida.

O Compose externo deve ter `docker:29.5.0-dind`, `DOCKER_TLS_CERTDIR=/certs`, volumes distintos para `docker-data`, `docker-certs` e `artifacts`, bind da worktree em `/workspace`, health check TLS e o runner `webtop` dependente da saúde do DinD.

- [ ] **Step 5: escrever e executar o teste de integração do spike**

`tests/integration/spike.sh` deve:

```bash
set -euo pipefail
test_root="$(mktemp -d)"
cleanup() {
  docker compose -p wktbox-spike-a --env-file "$test_root/a.env" -f assets/sandbox.compose.yml down --volumes --remove-orphans || true
  docker compose -p wktbox-spike-b --env-file "$test_root/b.env" -f assets/sandbox.compose.yml down --volumes --remove-orphans || true
  rm -rf "$test_root"
}
trap cleanup EXIT
```

O restante do script copia a fixture para dois diretórios, sobe os projetos externos, executa `docker compose up -d` dentro de cada runner, compara os IDs retornados por `docker ps`, valida que os project env diferem e confirma que derrubar A não interrompe B.

Run:

```bash
bash tests/integration/spike.sh
```

Expected: PASS com duas listas de containers internas disjuntas e a segunda box saudável após destruir a primeira.

- [ ] **Step 6: registrar os resultados duráveis do spike**

Atualizar `wktbox-plano-tecnico.md` somente com decisões comprovadas: compatibilidade de bind mount aninhado, modo Git escolhido, custo de startup/armazenamento e limitações específicas do host testado.

- [ ] **Step 7: commit**

```bash
git add .gitignore go.mod Makefile cmd internal assets images tests wktbox-plano-tecnico.md
git commit -m "feat: valida arquitetura inicial com dind isolado"
```

---

### Task 2: Descoberta da worktree e identidade estável

**Files:**
- Create: `internal/process/runner.go`
- Create: `internal/process/runner_test.go`
- Create: `internal/discovery/worktree.go`
- Create: `internal/discovery/worktree_test.go`
- Create: `internal/identity/identity.go`
- Create: `internal/identity/identity_test.go`

**Interfaces:**
- Produces: `process.Runner.Run(ctx context.Context, name string, args ...string) (process.Result, error)`.
- Produces: `discovery.Discover(ctx context.Context, runner process.Runner, path string) (discovery.Worktree, error)`.
- Produces: `identity.ForWorktree(commonDir, worktreePath string, platform identity.Platform) identity.BoxIdentity`.

- [ ] **Step 1: escrever testes RED**

```go
func TestForWorktreeUsesCommonDirAndCanonicalPath(t *testing.T) {
	got := identity.ForWorktree(`C:\Repo\.git`, `C:\Repo Trees\Feature`, identity.Windows)
	if got.ID != "29c42bf6e20f" {
		t.Fatalf("ID = %s", got.ID)
	}
	if got.ProjectName != "wktbox-29c42bf6e20f" {
		t.Fatalf("ProjectName = %s", got.ProjectName)
	}
}
```

```go
func TestDiscoverRejectsDirectoryOutsideGit(t *testing.T) {
	_, err := discovery.Discover(context.Background(), fakeRunner{
		result: process.Result{ExitCode: 128, Stderr: "not a git repository"},
	}, t.TempDir())
	if !errors.Is(err, discovery.ErrNotWorktree) {
		t.Fatalf("error = %v", err)
	}
}
```

- [ ] **Step 2: confirmar RED**

Run:

```bash
go test ./internal/identity ./internal/discovery ./internal/process
```

Expected: FAIL por pacotes ou símbolos ausentes.

- [ ] **Step 3: implementar canonicalização e descoberta**

`Discover` executa, sem shell:

```text
git -C <path> rev-parse --show-toplevel
git -C <path> rev-parse --git-common-dir
git -C <path> rev-parse --git-dir
git -C <path> branch --show-current
```

No Windows, `identity.ForWorktree` converte `\` para `/`, limpa o caminho e usa caixa minúscula antes de calcular SHA-256 sobre `<common>\n<worktree>`. Em Unix, preserva caixa e usa `filepath.EvalSymlinks` quando o caminho existe.

- [ ] **Step 4: confirmar GREEN**

Run:

```bash
go test ./internal/identity ./internal/discovery ./internal/process
```

Expected: PASS.

- [ ] **Step 5: commit**

```bash
git add internal/process internal/discovery internal/identity
git commit -m "feat: descobre worktrees e calcula identidade estavel"
```

---

### Task 3: Configuração e project env sem vazamento

**Files:**
- Create: `internal/config/model.go`
- Create: `internal/config/load.go`
- Create: `internal/config/load_test.go`
- Create: `internal/environment/project.go`
- Create: `internal/environment/project_test.go`
- Create: `internal/redact/redact.go`
- Create: `internal/redact/redact_test.go`

**Interfaces:**
- Produces: `config.Load(path string, environ map[string]string, flags config.Overrides) (config.Config, error)`.
- Produces: `environment.Resolve(worktree string, cfg config.Config, cliFile, cliTarget string) (environment.ProjectEnv, error)`.
- Produces: `redact.New(values ...string).String(input string) string`.

- [ ] **Step 1: escrever testes de precedência e validação**

```go
func TestLoadPrecedence(t *testing.T) {
	// defaults < .wktbox.yml < .wktbox.local.yml < WKTBOX_* < flags
	got, err := config.Load(fixtureDir, map[string]string{
		"WKTBOX_ENV_FILE": "from-env.env",
	}, config.Overrides{EnvFile: "from-flag.env"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Environment.File != "from-flag.env" {
		t.Fatalf("file = %q", got.Environment.File)
	}
}
```

```go
func TestResolveRejectsTargetOutsideAllowedRoots(t *testing.T) {
	_, err := environment.Resolve(t.TempDir(), config.Default(), "project.env", "/etc/profile")
	if !errors.Is(err, environment.ErrInvalidTarget) {
		t.Fatalf("error = %v", err)
	}
}
```

```go
func TestRedactorRemovesSecretValues(t *testing.T) {
	got := redact.New("s3cr3t").String("TOKEN=s3cr3t")
	if got != "TOKEN=***" {
		t.Fatalf("redacted = %q", got)
	}
}
```

- [ ] **Step 2: confirmar RED**

Run:

```bash
go test ./internal/config ./internal/environment ./internal/redact
```

Expected: FAIL por símbolos ausentes.

- [ ] **Step 3: implementar modelo estrito e resolução**

Usar `yaml.Load(data, &cfg, yaml.WithKnownFields(), yaml.WithUniqueKeys())`. Aplicar defaults concretos:

```go
Runtime.DindImage = "docker:29.5.0-dind"
Runtime.WebtopImage = "wktbox/webtop:dev"
Workspace.Target = "/workspace"
Environment.Target = "/workspace/.env"
Environment.ReadOnly = true
Webtop.Enabled = true
Webtop.SHMSize = "1gb"
Git.Mode = "host"
Resources.PIDs = 2048
```

`environment.Resolve` usa a precedência do plano, torna o caminho fonte absoluto, verifica arquivo regular e legível, não lê seu conteúdo e reconhece quando a fonte já é o arquivo de destino dentro da worktree.

- [ ] **Step 4: confirmar GREEN**

Run:

```bash
go test ./internal/config ./internal/environment ./internal/redact
```

Expected: PASS.

- [ ] **Step 5: commit**

```bash
git add internal/config internal/environment internal/redact go.mod go.sum
git commit -m "feat: carrega configuracao e protege project env"
```

---

### Task 4: Estado atômico, locks e blocos de portas

**Files:**
- Create: `internal/state/model.go`
- Create: `internal/state/store.go`
- Create: `internal/state/store_test.go`
- Create: `internal/lock/lock.go`
- Create: `internal/lock/lock_test.go`
- Create: `internal/ports/allocator.go`
- Create: `internal/ports/allocator_test.go`

**Interfaces:**
- Produces: `state.Store.Load(context.Context) (state.State, error)`.
- Produces: `state.Store.Save(context.Context, state.State) error`.
- Produces: `lock.Manager.Global(ctx context.Context) (func() error, error)` e `Box(ctx context.Context, id string)`.
- Produces: `ports.Allocator.Reserve(ctx context.Context, used []ports.Block) (ports.Block, error)`.

- [ ] **Step 1: escrever testes RED**

```go
func TestSaveIsAtomicAndRoundTrips(t *testing.T) {
	store := state.NewStore(t.TempDir())
	want := state.State{Version: 1, Boxes: map[string]state.BoxRecord{
		"abc": {ID: "abc", Worktree: "/repo", Status: state.Ready},
	}}
	if err := store.Save(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("state = %#v, want %#v", got, want)
	}
}
```

```go
func TestReserveSkipsOccupiedPortInCandidateBlock(t *testing.T) {
	listener, _ := net.Listen("tcp", "127.0.0.1:23003")
	defer listener.Close()
	block, err := ports.NewAllocator(23000, 10).Reserve(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if block.Start != 23010 {
		t.Fatalf("start = %d", block.Start)
	}
}
```

- [ ] **Step 2: confirmar RED**

Run:

```bash
go test ./internal/state ./internal/lock ./internal/ports
```

Expected: FAIL por símbolos ausentes.

- [ ] **Step 3: implementar**

O estado usa `state.json`, escrita em arquivo temporário no mesmo diretório, `Sync`, `Close` e `Rename`. Um JSON inválido retorna `state.ErrCorrupt`, sem ser silenciosamente substituído.

Os locks usam `flock.New(path).TryLockContext(ctx, 100*time.Millisecond)` e sempre retornam uma função idempotente de unlock.

O alocador tenta blocos crescentes, abre listeners temporários em todos os dez endereços `127.0.0.1:<porta>` e só retorna o bloco depois de fechar todos os listeners. A reserva é persistida enquanto o lock global permanece adquirido.

- [ ] **Step 4: confirmar GREEN e detector de race**

Run:

```bash
go test -race ./internal/state ./internal/lock ./internal/ports
```

Expected: PASS.

- [ ] **Step 5: commit**

```bash
git add internal/state internal/lock internal/ports go.mod go.sum
git commit -m "feat: persiste estado e reserva portas com locks"
```

---

### Task 5: Geração determinística do sandbox externo

**Files:**
- Modify: `assets/sandbox.compose.yml`
- Create: `internal/sandbox/spec.go`
- Create: `internal/sandbox/render.go`
- Create: `internal/sandbox/render_test.go`

**Interfaces:**
- Produces: `sandbox.Render(dir string, spec sandbox.Spec) (sandbox.Files, error)`.
- `sandbox.Files` contém `ComposePath`, `SandboxEnvPath` e `ProjectEnvOverridePath`.

- [ ] **Step 1: escrever testes RED**

```go
func TestRenderMountsWorkspaceInDockerAndWebtop(t *testing.T) {
	files, err := sandbox.Render(t.TempDir(), sandbox.Spec{
		ID: "a4f8c9137d2b", Worktree: "/repo tree",
		Ports: ports.Block{Start: 23000},
		Config: config.Default(),
	})
	if err != nil {
		t.Fatal(err)
	}
	compose := readFile(t, files.ComposePath)
	for _, service := range []string{"docker", "webtop"} {
		assertServiceBind(t, compose, service, "/repo tree", "/workspace", false)
	}
	if strings.Contains(compose, "/var/run/docker.sock") {
		t.Fatal("host Docker socket leaked into sandbox")
	}
}
```

```go
func TestRenderProjectEnvOverrideContainsPathNotContent(t *testing.T) {
	secretFile := filepath.Join(t.TempDir(), "project.env")
	os.WriteFile(secretFile, []byte("TOKEN=do-not-copy"), 0o600)
	files, err := sandbox.Render(t.TempDir(), sandbox.Spec{
		ProjectEnv: environment.ProjectEnv{Source: secretFile, Target: "/workspace/.env", Mount: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := readFile(t, files.ProjectEnvOverridePath)
	if strings.Contains(got, "do-not-copy") {
		t.Fatal("project env content leaked")
	}
}
```

- [ ] **Step 2: confirmar RED**

Run:

```bash
go test ./internal/sandbox
```

Expected: FAIL por pacote ausente.

- [ ] **Step 3: implementar renderização**

Incorporar o template com `//go:embed`. Gerar YAML por estruturas tipadas, não por concatenação de caminhos. Todas as labels `io.wktbox.managed`, `io.wktbox.box-id`, `io.wktbox.worktree` e `io.wktbox.version` aparecem nos três serviços. O override do project env adiciona o mesmo bind `read_only: true` a `docker` e `webtop`.

O `sandbox.env` contém somente metadados não secretos:

```text
WKTBOX_ID
WORKTREE_PATH
PORT_HTTP
PORT_HTTPS
PORT_SSH
PORT_GATEWAY
TZ
PUID
PGID
SHM_SIZE
WKTBOX_DIND_IMAGE
WKTBOX_WEBTOP_IMAGE
```

- [ ] **Step 4: validar GREEN e Compose**

Run:

```bash
go test ./internal/sandbox
```

Expected: PASS; o próprio teste executa `docker compose config` sobre os arquivos temporários antes de removê-los, e nenhum secret aparece no diretório da box.

- [ ] **Step 5: commit**

```bash
git add assets internal/sandbox
git commit -m "feat: gera sandbox externo sem expor segredos"
```

---

### Task 6: Backend Compose e reconciliação pelo Docker real

**Files:**
- Create: `internal/compose/client.go`
- Create: `internal/compose/client_test.go`
- Create: `internal/sandbox/manager.go`
- Create: `internal/sandbox/manager_test.go`

**Interfaces:**
- Produces: `compose.Client.Up`, `Stop`, `Restart`, `Down`, `Exec`, `Logs`, `Ps` e `InspectByLabels`.
- Produces: `sandbox.Manager.Ensure`, `Start`, `Stop`, `Restart`, `Destroy`, `Inspect` e `List`.

- [ ] **Step 1: escrever testes RED com runner falso**

```go
func TestDownRemovesVolumesOnlyForSelectedProject(t *testing.T) {
	runner := &recordingRunner{}
	client := compose.NewClient(runner)
	err := client.Down(context.Background(), compose.Project{
		Name: "wktbox-a4f8c9137d2b", Files: []string{"/state/a/compose.yml"},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	runner.AssertArgs(t, "docker", "compose", "-p", "wktbox-a4f8c9137d2b",
		"-f", "/state/a/compose.yml", "down", "--volumes", "--remove-orphans")
}
```

```go
func TestInspectPrefersDockerStatusOverCachedStatus(t *testing.T) {
	manager := sandbox.NewManager(fakeCompose{status: compose.Status{State: "running", Health: "healthy"}}, storeWithStatus(state.Stopped))
	got, err := manager.Inspect(context.Background(), "a4f8c9137d2b")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != state.Ready {
		t.Fatalf("status = %s", got.Status)
	}
}
```

- [ ] **Step 2: confirmar RED**

Run:

```bash
go test ./internal/compose ./internal/sandbox
```

Expected: FAIL pelos novos símbolos.

- [ ] **Step 3: implementar sem shell**

Cada chamada Docker usa `exec.CommandContext` com argumentos separados. `Ensure` adquire lock da box, renderiza arquivos, executa `docker compose up -d --wait`, inspeciona saúde real e só então grava `Ready`. `Stop` usa `docker compose stop`; `Destroy` usa `down --volumes --remove-orphans`, remove apenas `boxes/<id>` e a entrada do estado.

Reconciliação consulta containers com:

```text
docker ps -a --filter label=io.wktbox.managed=true --format {{json .}}
```

e nunca considera um cache `Ready` suficiente para declarar saúde.

- [ ] **Step 4: confirmar GREEN**

Run:

```bash
go test -race ./internal/compose ./internal/sandbox
```

Expected: PASS.

- [ ] **Step 5: commit**

```bash
git add internal/compose internal/sandbox
git commit -m "feat: controla e reconcilia boxes pelo docker"
```

---

### Task 7: Execução fiel de comandos

**Files:**
- Create: `internal/executor/executor.go`
- Create: `internal/executor/executor_test.go`
- Create: `internal/executor/signals_unix.go`
- Create: `internal/executor/signals_windows.go`

**Interfaces:**
- Produces: `executor.Executor.Run(ctx context.Context, box state.BoxRecord, command []string, opts executor.Options) (int, error)`.
- Produces: `executor.MapWorkingDirectory(worktree, hostCWD string) (string, error)`.

- [ ] **Step 1: escrever testes RED**

```go
func TestMapWorkingDirectoryPreservesRelativeSubdirectory(t *testing.T) {
	got, err := executor.MapWorkingDirectory("/repo", "/repo/frontend/src")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/workspace/frontend/src" {
		t.Fatalf("cwd = %q", got)
	}
}
```

```go
func TestRunForwardsArgumentsWithoutShellReconstruction(t *testing.T) {
	runner := &recordingInteractiveRunner{}
	exec := executor.New(runner)
	_, err := exec.Run(context.Background(), testBox(), []string{
		"printf", "%s", "a b", "$(must-not-expand)",
	}, executor.Options{TTY: false})
	if err != nil {
		t.Fatal(err)
	}
	runner.AssertChildArgs(t, "printf", "%s", "a b", "$(must-not-expand)")
}
```

- [ ] **Step 2: confirmar RED**

Run:

```bash
go test ./internal/executor
```

Expected: FAIL por pacote ausente.

- [ ] **Step 3: implementar**

Construir:

```text
docker compose -p <project> -f <compose> exec
  [--env KEY=VALUE]...
  [-T quando não houver TTY]
  -w <cwd>
  webtop
  <argv original...>
```

Conectar diretamente `os.Stdin`, `os.Stdout`, `os.Stderr`; não envolver o filho em `sh -c`. Em Unix, colocar o processo Docker no mesmo grupo de processo e encaminhar `SIGINT`, `SIGTERM` e `SIGHUP`. Em Windows, cancelar o processo com `os.Interrupt` e preservar seu exit code. Um `*exec.ExitError` retorna `ExitCode()` sem converter a saída do filho em erro da CLI.

- [ ] **Step 4: confirmar GREEN**

Run:

```bash
go test ./internal/executor
```

Expected: PASS.

- [ ] **Step 5: commit**

```bash
git add internal/executor
git commit -m "feat: preserva argumentos tty sinais e codigo de saida"
```

---

### Task 8: CLI completa e saída JSON

**Files:**
- Modify: `cmd/wktbox/main.go`
- Create: `internal/app/app.go`
- Create: `internal/cli/root.go`
- Create: `internal/cli/root_test.go`
- Create: `internal/output/output.go`
- Create: `internal/output/output_test.go`

**Interfaces:**
- Produces: `cli.New(app *app.App, streams cli.Streams) *cobra.Command`.
- Produces comandos `up`, `run`, `exec`, `compose`, `shell`, `open`, `list`, `status`, `logs`, `stop`, `restart`, `destroy` e `doctor`.

- [ ] **Step 1: escrever testes RED de contrato**

```go
func TestRunEnsuresBoxThenPassesChildArgs(t *testing.T) {
	fake := newFakeApp()
	root := cli.New(fake, testStreams())
	root.SetArgs([]string{"--path", "/repo", "run", "--", "docker", "compose", "up", "-d"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	fake.AssertCalls(t, "Resolve:/repo", "Ensure", "Run:docker|compose|up|-d")
}
```

```go
func TestExecStoppedBoxReturnsActionableError(t *testing.T) {
	fake := newFakeApp()
	fake.status = state.Stopped
	root := cli.New(fake, testStreams())
	root.SetArgs([]string{"exec", "--", "pwd"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), `wktbox up`) || !strings.Contains(err.Error(), `wktbox run --`) {
		t.Fatalf("error = %v", err)
	}
}
```

```go
func TestDestroyRequiresForceWithoutTerminal(t *testing.T) {
	fake := newFakeApp()
	root := cli.New(fake, nonInteractiveStreams())
	root.SetArgs([]string{"destroy"})
	err := root.Execute()
	if !errors.Is(err, cli.ErrForceRequired) {
		t.Fatalf("error = %v", err)
	}
}
```

- [ ] **Step 2: confirmar RED**

Run:

```bash
go test ./internal/cli ./internal/output
```

Expected: FAIL por pacotes ausentes.

- [ ] **Step 3: implementar comandos e flags**

Flags persistentes: `--path`, `--config`, `--env-file`, `--env-target`, `--profile`, `--json`, `--quiet`, `--verbose`, `--no-color` e repetível `--env KEY=VALUE`.

Semântica exata:

- `up`: resolve e garante infraestrutura; não inicia Compose interno.
- `run`: garante infraestrutura e executa argv.
- `exec`: exige estado real Ready.
- `compose`: chama `run` com prefixo `docker compose`.
- `shell`: usa `bash`, fallback `sh`.
- `open`: abre somente URL loopback já conhecida.
- `list` e `status`: reconciliam Docker antes da saída.
- `logs`: serviço opcional e `--follow`.
- `stop`, `restart`, `destroy`: usam lock da box; `destroy` exige confirmação ou `--force`.
- `doctor`: chama o componente da Task 9.

JSON usa objetos estáveis e envia erros como `{"error":{"code":"...","message":"..."}}` para stderr. Saída humana nunca contém conteúdo do project env ou valores de `--env`.

- [ ] **Step 4: confirmar GREEN e construir**

Run:

```bash
go test ./internal/cli ./internal/output
go build -o bin/wktbox ./cmd/wktbox
./bin/wktbox --help
```

Expected: PASS; help lista todos os comandos MVP.

- [ ] **Step 5: commit**

```bash
git add cmd internal/app internal/cli internal/output
git commit -m "feat: entrega contrato completo da cli"
```

---

### Task 9: Doctor e ponte Git explícita

**Files:**
- Create: `internal/doctor/doctor.go`
- Create: `internal/doctor/doctor_test.go`
- Create: `internal/gitbridge/gitbridge.go`
- Create: `internal/gitbridge/gitbridge_test.go`

**Interfaces:**
- Produces: `doctor.Run(ctx context.Context, input doctor.Input) doctor.Report`.
- Produces: `gitbridge.Prepare(worktree discovery.Worktree, mode config.GitMode) (gitbridge.Mounts, gitbridge.Environment, error)`.

- [ ] **Step 1: escrever testes RED**

```go
func TestDoctorReportsAllChecksInsteadOfStoppingAtFirstFailure(t *testing.T) {
	report := doctor.Run(context.Background(), failingProbeSet())
	if len(report.Checks) != doctor.RequiredCheckCount {
		t.Fatalf("checks = %d", len(report.Checks))
	}
	if report.OK {
		t.Fatal("report should fail")
	}
}
```

```go
func TestHostModeDoesNotMountSharedGitMetadata(t *testing.T) {
	mounts, env, err := gitbridge.Prepare(testLinkedWorktree(), config.GitHost)
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 0 || len(env) != 0 {
		t.Fatalf("host mode exposed git metadata")
	}
}
```

- [ ] **Step 2: confirmar RED**

Run:

```bash
go test ./internal/doctor ./internal/gitbridge
```

Expected: FAIL por pacotes ausentes.

- [ ] **Step 3: implementar**

Checks obrigatórios: Docker disponível, Compose v2, daemon Linux, bind mount, worktree válida, project env válido, bloco de portas, DinD privilegiado, espaço em disco, TLS DinD e ponte Git.

`git.mode: host` não monta metadata e emite limitação estruturada. `git.mode: mounted` monta o common dir em `/wktbox/git-common`, configura `GIT_WORK_TREE`, `GIT_DIR` e `GIT_COMMON_DIR`, mas só é aceito depois de `git status`, `git diff --quiet` e `git rev-parse --git-common-dir` passarem dentro do runner. Falha de validação cai para erro acionável, nunca para montagem silenciosa em modo host.

- [ ] **Step 4: confirmar GREEN**

Run:

```bash
go test ./internal/doctor ./internal/gitbridge
```

Expected: PASS.

- [ ] **Step 5: commit**

```bash
git add internal/doctor internal/gitbridge
git commit -m "feat: diagnostica host e explicita ponte git"
```

---

### Task 10: Webtop e gateway HTTP opcional

**Files:**
- Modify: `images/webtop/Dockerfile`
- Create: `images/gateway/Dockerfile`
- Create: `images/gateway/nginx.conf.template`
- Create: `internal/gateway/config.go`
- Create: `internal/gateway/config_test.go`
- Modify: `assets/sandbox.compose.yml`

**Interfaces:**
- Produces: imagem `wktbox/webtop:<version>` com terminal Webtop e Docker CLI/Compose compatíveis com TLS.
- Produces: `gateway.Render(routes map[string]config.Route, boxID string) ([]byte, error)`.

- [ ] **Step 1: escrever testes RED do gateway**

```go
func TestRenderUsesPerBoxHostsAndDockerUpstreams(t *testing.T) {
	got, err := gateway.Render(map[string]config.Route{
		"frontend": {Port: 5173},
		"api": {Port: 8000},
	}, "a4f8c9137d2b")
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, got, "frontend.a4f8c9137d2b.localhost")
	assertContains(t, got, "proxy_pass http://docker:5173")
	assertContains(t, got, "proxy_set_header Upgrade $http_upgrade")
}
```

- [ ] **Step 2: confirmar RED**

Run:

```bash
go test ./internal/gateway
```

Expected: FAIL por pacote ausente.

- [ ] **Step 3: implementar imagens e configuração**

O Webtop publica 3000/3001 apenas por mapeamentos loopback definidos no Compose externo, mantém `/config`, usa `/workspace`, não contém senha padrão e não recebe credenciais Git/SSH/Docker automaticamente.

O gateway publica apenas 8080 em loopback, gera um `server_name` por rota, encaminha `X-Forwarded-For`, `X-Forwarded-Host`, `X-Forwarded-Proto`, `Upgrade` e `Connection`, e não expõe administração.

- [ ] **Step 4: confirmar GREEN e smoke tests**

Run:

```bash
go test ./internal/gateway
docker build -t wktbox/webtop:dev images/webtop
docker build -t wktbox/gateway:dev images/gateway
docker run --rm wktbox/webtop:dev docker compose version
```

Expected: PASS e Compose v2 disponível no Webtop.

- [ ] **Step 5: commit**

```bash
git add images assets internal/gateway
git commit -m "feat: adiciona webtop e gateway por box"
```

---

### Task 11: Integração e E2E obrigatório em duas worktrees

**Files:**
- Create: `tests/integration/cli_test.go`
- Create: `tests/e2e/two_worktrees.sh`
- Create: `tests/e2e/assert-isolation.sh`
- Create: `tests/e2e/measure.sh`

**Interfaces:**
- Produces: suíte que executa o binário real contra Docker real.
- Produces: evidência de IDs, containers, redes, volumes, portas, env e destruição seletiva.

- [ ] **Step 1: escrever o E2E antes de completar os ajustes finais**

O script cria um repositório temporário e duas linked worktrees, copia o mesmo Compose com serviços nas portas 5173, 8000 e 5432, e executa:

```bash
wktbox --path "$worktree_a" --env-file "$env_a" run -- docker compose up -d
wktbox --path "$worktree_b" --env-file "$env_b" run -- docker compose up -d
wktbox --path "$worktree_a" exec -- docker compose run --rm e2e
wktbox --path "$worktree_b" exec -- docker compose run --rm e2e
```

Depois compara:

```bash
ps_a="$(wktbox --path "$worktree_a" exec -- docker ps --format '{{.ID}}')"
ps_b="$(wktbox --path "$worktree_b" exec -- docker ps --format '{{.ID}}')"
test "$ps_a" != "$ps_b"
```

e confirma que `destroy --force` de A não altera `status --json` nem o E2E de B.

- [ ] **Step 2: confirmar que o E2E detecta pelo menos uma lacuna**

Run:

```bash
bash tests/e2e/two_worktrees.sh
```

Expected: FAIL em uma lacuna real ainda não coberta; registrar exatamente o requisito quebrado.

- [ ] **Step 3: corrigir apenas as lacunas expostas**

Aplicar ciclos RED/GREEN separados para cada falha: paths com espaços, env read-only, duas portas iguais, estado perdido, processo interrompido, duas alocações simultâneas, stop/start persistente e destroy seletivo.

- [ ] **Step 4: executar suíte completa**

Run:

```bash
go test -race ./...
bash tests/integration/spike.sh
bash tests/e2e/two_worktrees.sh
```

Expected: todos PASS, sem containers, redes ou volumes de teste restantes após traps.

- [ ] **Step 5: coletar métricas**

Run:

```bash
bash tests/e2e/measure.sh
```

Expected: JSON com tempo de cold/warm startup, CPU, RAM e bytes de volumes para duas boxes; nenhum secret.

- [ ] **Step 6: commit**

```bash
git add tests internal cmd assets images
git commit -m "fix: cobre isolamento concorrencia e recuperacao e2e"
```

---

### Task 12: Documentação, builds multiplataforma e auditoria de aceite

**Files:**
- Create: `README.md`
- Create: `docs/configuration.md`
- Create: `docs/security.md`
- Create: `docs/windows.md`
- Create: `.github/workflows/ci.yml`
- Create: `.github/workflows/release.yml`
- Create: `scripts/build.sh`
- Modify: `wktbox-plano-tecnico.md`

**Interfaces:**
- Produces: binários `wktbox` para Windows amd64/arm64, Linux amd64/arm64 e macOS amd64/arm64.
- Produces: documentação que distingue isolamento operacional de sandbox de segurança.

- [ ] **Step 1: escrever teste de documentação**

Criar `tests/docs_test.go` que verifica:

```go
func TestSecurityDocumentationDoesNotPromiseUntrustedCodeSafety(t *testing.T) {
	for _, path := range []string{"README.md", "docs/security.md", "docs/windows.md"} {
		body := read(t, path)
		for _, forbidden := range []string{"VM-equivalent isolation", "execução segura de código não confiável"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s contains forbidden claim %q", path, forbidden)
			}
		}
	}
}
```

- [ ] **Step 2: confirmar RED**

Run:

```bash
go test ./tests
```

Expected: FAIL porque a documentação ainda não existe.

- [ ] **Step 3: documentar instalação, contrato, segurança e Windows**

README inclui quick start, todos os comandos, saída JSON e exemplo de duas worktrees. `docs/configuration.md` descreve cada campo e precedência. `docs/security.md` explica DinD privilegiado, loopback, TLS, ausência de socket host e tratamento de secrets. `docs/windows.md` cobre Docker Desktop, drives compartilhados, rejeição de UNC, caminhos com espaços, UID/GID e limitações do modo Git.

- [ ] **Step 4: construir e testar matrizes**

Run:

```bash
go test -race ./...
GOOS=windows GOARCH=amd64 go build -o dist/wktbox-windows-amd64.exe ./cmd/wktbox
GOOS=linux GOARCH=amd64 go build -o dist/wktbox-linux-amd64 ./cmd/wktbox
GOOS=darwin GOARCH=arm64 go build -o dist/wktbox-darwin-arm64 ./cmd/wktbox
```

Expected: PASS e três binários gerados.

- [ ] **Step 5: auditar os 17 critérios de aceite**

Adicionar ao fim de `wktbox-plano-tecnico.md` uma tabela `Critério → teste/evidência`. Nenhum item pode usar apenas inspeção indireta quando há comportamento executável; os itens 5, 6, 8, 10, 13, 14 e 15 apontam para o E2E de duas worktrees.

- [ ] **Step 6: verificação final**

Run:

```bash
go vet ./...
go test -race ./...
bash tests/integration/spike.sh
bash tests/e2e/two_worktrees.sh
git diff --check
git status --short
```

Expected: todos os comandos passam; `git status --short` contém somente arquivos deliberadamente não commitados, ou fica vazio após o commit.

- [ ] **Step 7: commit**

```bash
git add README.md docs .github scripts tests wktbox-plano-tecnico.md
git commit -m "feat: documenta e verifica o mvp multiplataforma"
```
