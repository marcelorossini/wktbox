# Configuração

[English canonical guide](../configuration.md).

O Wktbox usa schema `version: 1`. A precedência, da menor para a maior, é:

```text
defaults < .wktbox.yml < .wktbox.local.yml < WKTBOX_* < flags
```

Um `--config` explícito substitui os dois arquivos automáticos. Caminhos
relativos são resolvidos a partir do workspace. `.wktbox.local.yml` deve ficar no
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
ainda não instala nem configura um servidor SSH. O offset reservado a SSH não é
publicado pelo Compose externo.

### Chromium gráfico e CDP

O Chromium gráfico permanece aberto em toda box pronta. O XFCE inicia um
supervisor da sessão que reabre o navegador quando ele termina. O perfil
persistente fica em `/config/.config/wktbox-chromium`, e o CDP escuta na porta
de loopback `9222`. Um relay supervisionado pelo s6 escuta na porta interna
`9223` e encaminha a publicação do host ao Chromium.

A porta do host usa o offset `+5` do bloco de dez portas da box e é vinculada
somente a `127.0.0.1`. Em um bloco iniciado em `23000`, o HTTP do Webtop usa
`23000` e o CDP usa `23005`. Integrações devem consultar `urls.browserCdp` ou
`ports.browserCdp` em `wktbox --json status`, sem derivar a porta.

### Localhost automático

O localhost automático não possui seção de configuração. O `ports:` do Compose
interno é a fonte de verdade, e o sidecar consulta `NetworkSettings.Ports` do
DinD. Somente bindings TCP com `HostPort` são encaminhados. Em
`"8000:3000"`, a rota exposta no Webtop é `localhost:8000 -> docker:8000`;
`EXPOSE 3000` sem publicação não cria rota. UDP aparece como aviso no status.

O Webtop reserva internamente 61000 para HTTP, 61001 para HTTPS e 61002 para
WebSocket. Isso libera portas comuns de desenvolvimento, como 3000 e 3001, para
as aplicações. Se um container interno publicar uma das portas reservadas, o
status registra `conflict` e as demais rotas permanecem ativas.

`wktbox run` e `wktbox compose` solicitam sync depois de um processo filho
bem-sucedido. Eventos `start`, `die`, `stop`, `destroy` e `rename` também
atualizam as rotas automaticamente. Consulte o estado atual com:

```bash
wktbox status
wktbox --json status
```

`git.mode` aceita:

- `host` (default): operações Git permanecem no host; a box recebe apenas o
  workspace.
- `mounted`: monta os metadados comuns da linked worktree no Webtop com escrita
  e valida `git status`, `git diff` e `git rev-parse`. Não monta metadados no
  DinD. O modo é ignorado com um aviso do doctor quando o workspace não possui
  Git ou está abaixo da raiz Git. Use somente após validar o host e os hooks do
  repositório.

`resources` (`cpus`, `memory`, `pids`) é reservado no schema v1. Os valores são
lidos, mas ainda não impõem limites; não os trate como controle de segurança.

`gateway.enabled` ativa o perfil externo `gateway`. Ele é opcional e
independente do localhost automático dentro do Webtop. Cada entrada em
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
`--config` escolhe um arquivo alternativo. `--path` seleciona exatamente o
diretório do workspace; uma raiz Git detectada não substitui esse caminho.
`--env KEY=VALUE` não altera a configuração persistente: só é repassado ao
comando filho.

A resolução é declarativa e ocorre em toda invocação. Um `--env-file` usado
anteriormente não vira default persistente: repita a flag ao reconciliar a box
ou registre `environment.file` em `.wktbox.local.yml`. Sem flag, variável,
configuração ou `.env` no workspace, o resultado intencional é nenhum project
env.

O flag `--profile` existe para compatibilidade futura, mas qualquer valor
nomeado é rejeitado explicitamente no schema v1.

O encaminhamento bidirecional também não possui seção YAML. Os mapeamentos
persistentes são administrados explicitamente com `wktbox port import`,
`wktbox port publish`, `wktbox port list` e `wktbox port remove`. Importações
preservam serviços do host como `localhost` dos workloads; publicações escutam
somente no loopback do host. Consulte
[Encaminhamento bidirecional de portas](port-forwarding.md).
