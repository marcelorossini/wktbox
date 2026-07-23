# Configuração

O Wktbox usa schema `version: 1`. A precedência, da menor para a maior, é:

```text
defaults < .wktbox.yml < .wktbox.local.yml < WKTBOX_* < flags
```

Um `--config` explícito substitui os dois arquivos automáticos. Caminhos
relativos são resolvidos a partir da worktree. `.wktbox.local.yml` deve ficar no
`.gitignore`.

## Exemplo

```yaml
version: 1

workspace:
  target: /workspace

environment:
  file: .env
  target: /workspace/.env
  readOnly: true

runtime:
  dindImage: docker:29.5.0-dind
  webtopImage: wktbox/webtop:dev
  gatewayImage: wktbox/gateway:dev

webtop:
  enabled: true
  shmSize: 2gb
  ssh:
    enabled: false

git:
  mode: host

resources:
  cpus: 4
  memory: 8gb
  pids: 2048

gateway:
  enabled: true
  routes:
    frontend:
      port: 5173
    api:
      port: 8000

commands:
  e2e:
    command: [docker, compose, run, --rm, e2e]

lifecycle:
  idleTimeout: 0
  removeOnWorktreeDelete: false
```

## Seções

`workspace.target` é fixo em `/workspace` no MVP. A igualdade do caminho dentro
do cliente Webtop e do daemon DinD permite que bind mounts relativos do Compose
interno funcionem.

`environment.file` escolhe o project env. Se existir, ele é montado em
`environment.target`, por padrão `/workspace/.env`, no Webtop e no DinD.
`environment.readOnly` deve permanecer `true`; o Wktbox não copia esse conteúdo
para o estado.

`runtime` fixa as imagens externas. Os defaults são
`docker:29.5.0-dind`, `wktbox/webtop:dev` e `wktbox/gateway:dev`. Uma mudança de
imagem ou de qualquer arquivo externo gerado é reaplicada por `wktbox up`.

`webtop.enabled` deve ser `true` no MVP. `webtop.shmSize` configura a memória
compartilhada do desktop. `webtop.ssh.enabled: true` é rejeitado porque o produto
ainda não instala nem configura um servidor SSH.

`git.mode` aceita:

- `host` (default): operações Git permanecem no host; a box recebe apenas a
  worktree.
- `mounted`: monta os metadados comuns da linked worktree no Webtop com escrita
  e valida `git status`, `git diff` e `git rev-parse`. Não monta metadados no
  DinD. Use somente após validar o host e os hooks do repositório.

`resources` (`cpus`, `memory`, `pids`) é reservado no schema v1. Os valores são
lidos, mas ainda não impõem limites; não os trate como controle de segurança.

`gateway.enabled` ativa o perfil externo `gateway`. Cada entrada em
`gateway.routes` tem nome DNS minúsculo e uma porta interna entre 1 e 65535. A
rota `<nome>.<box-id>.localhost` aponta para `docker:<porta>`, com WebSockets.

`commands` é reservado no schema v1. Os atalhos nomeados são lidos, mas não são
executados automaticamente; use `wktbox run -- ...` ou `wktbox compose -- ...`.

`lifecycle` também é reservado. `idleTimeout` e `removeOnWorktreeDelete` não
disparam limpeza automática no MVP.

## Variáveis e flags

Variáveis implementadas:

| Variável | Efeito |
|---|---|
| `WKTBOX_ENV_FILE` | arquivo de project env |
| `WKTBOX_ENV_TARGET` | destino do project env |
| `WKTBOX_DIND_IMAGE` | imagem DinD |
| `WKTBOX_WEBTOP_IMAGE` | imagem Webtop |
| `WKTBOX_GATEWAY_IMAGE` | imagem gateway |
| `WKTBOX_STATE_HOME` | diretório do cache, locks e arquivos externos |

As flags `--env-file` e `--env-target` vencem as variáveis correspondentes.
`--config` escolhe um arquivo alternativo. `--path` escolhe a worktree.
`--env KEY=VALUE` não altera a configuração persistente: só é repassado ao
comando filho.

O flag `--profile` existe para compatibilidade futura, mas qualquer valor
nomeado é rejeitado explicitamente no schema v1.
