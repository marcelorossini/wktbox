# Wktbox

Esta é a documentação em português. Consulte a
[documentação canônica em inglês](README.md).

Wktbox é uma CLI em Go que cria uma box de desenvolvimento por Git worktree.
Cada box usa um daemon Docker-in-Docker (DinD) exclusivo, preserva as portas
internas do projeto e oferece Webtop, execução de comandos, localhost automático
para serviços publicados e gateway HTTP opcional.

O mecanismo fornece **isolamento operacional** entre ambientes de
desenvolvimento. O DinD é privilegiado e a worktree é montada com escrita; não
use o Wktbox como barreira para código hostil. Consulte
[Segurança](docs/pt-BR/security.md).

## Requisitos

- Git;
- Docker Engine ou Docker Desktop em modo de containers Linux;
- Docker CLI com Compose v2;
- permissão para executar containers privilegiados;
- em Windows, uma unidade local compartilhada com o Docker Desktop.

Antes da primeira box:

```bash
wktbox doctor --path "/caminho/da/worktree"
```

## Compilação local

O `Makefile` usa a imagem `golang:1.26.5`, portanto não exige Go instalado no
host:

```bash
make build
docker build -f images/webtop/Dockerfile -t wktbox/webtop:dev .
docker build -t wktbox/gateway:dev images/gateway
```

O binário fica em `bin/wktbox`. Para gerar os seis artefatos de release:

```bash
./scripts/build.sh 0.1.0
```

## Início rápido

Dentro de uma worktree:

```bash
wktbox up
wktbox run -- docker compose up --build -d
wktbox exec -- docker compose ps
wktbox open
```

`run` cria ou inicia a box antes do comando. `exec` exige que ela já esteja
pronta. A worktree aparece em `/workspace` tanto no Webtop quanto no DinD; o
Compose original roda sem reescrita de portas.

## Localhost automático dentro do Webtop

O `ports:` do Compose interno é a fonte de verdade. Para cada porta TCP
realmente publicada por um container em execução, o Wktbox cria a mesma rota no
loopback do Webtop. Não há configuração adicional:

```yaml
services:
  frontend:
    ports:
      - "5173:5173"
  api:
    ports:
      - "8000:3000"
```

Dentro daquela box, `http://localhost:5173` chega ao frontend e
`http://localhost:8000` chega à porta 3000 da API. A porta publicada à esquerda
é preservada; `EXPOSE` sem publicação não cria rota. Duas boxes podem usar o
mesmo `localhost:5173` porque cada Webtop tem seu próprio namespace de rede.

O sidecar observa eventos do DinD. Containers criados ou removidos depois
atualizam as rotas sem reiniciar o Webtop. `wktbox run` e `wktbox compose`
também forçam um sync após o comando bem-sucedido e imprimem o resumo em stderr,
sem alterar o stdout do processo filho.

Use `wktbox status` ou `wktbox --json status` para ver `listening`, `conflict`,
origens e destino de cada rota. Uma publicação que colida com uma porta usada
pelo próprio Webtop aparece como `conflict` sem derrubar outras rotas. UDP é
reportado como aviso e não é encaminhado; o proxy automático é TCP e preserva
HTTP, HTTPS, WebSocket, hot reload e protocolos como PostgreSQL.

Para montar um arquivo externo como `/workspace/.env`, somente leitura:

```bash
wktbox --env-file "/segredos/feature.env" run -- docker compose up -d
```

Flags são resolvidas em cada invocação. Se a box precisar ser recriada ou
reconciliada, repita `--env-file` ou configure `environment.file` em
`.wktbox.local.yml`; omitir todas as fontes significa “nenhum project env”.

## duas worktrees em paralelo

Duas worktrees podem executar o mesmo Compose com as mesmas portas internas:

```bash
wktbox --path "../projeto feature-a" run -- docker compose up -d
wktbox --path "../projeto feature-b" run -- docker compose up -d
```

Cada comando resolve uma identidade estável, reserva um bloco de portas externas
e usa daemon, containers, redes, imagens e volumes próprios.

## Comandos

- `wktbox up`: cria, inicia ou reconcilia a infraestrutura externa.
- `wktbox run -- <comando>`: garante a box pronta e executa o comando.
- `wktbox exec -- <comando>`: executa somente em uma box já pronta.
- `wktbox compose -- <argumentos>`: atalho para `docker compose` interno.
- `wktbox shell`: abre Bash, com fallback para `sh`, em `/workspace`.
- `wktbox open`: abre a URL HTTP conhecida do Webtop.
- `wktbox list`: lista boxes e reconcilia o cache com os labels Docker.
- `wktbox status`: mostra estado, worktree, branch, portas, URLs e rotas
  automáticas do Webtop.
- `wktbox logs [serviço]`: mostra logs do Compose externo; aceita `--follow`.
- `wktbox stop`: para a box preservando volumes, imagens e dados internos.
- `wktbox restart`: reinicia a infraestrutura externa e valida a prontidão.
- `wktbox destroy`: remove apenas a box selecionada e seus volumes; em automação,
  use `--force`.
- `wktbox doctor`: verifica Git, Docker, Compose, bind mounts, portas, disco,
  DinD privilegiado, TLS e a ponte Git aplicável.

Flags globais relevantes:

```text
--path --config --env-file --env-target --env --json
--quiet --verbose --no-color
```

`--env KEY=VALUE` vale apenas para o processo filho e pode ser repetida. O flag
`--profile` está reservado e retorna erro no schema v1.

Para automação, `--json` produz objetos estáveis em stdout e erros estruturados
em stderr:

```bash
wktbox --json status
wktbox --json list
wktbox --json doctor
```

O código de saída do processo executado por `run`, `exec`, `compose` ou `shell`
é devolvido pela CLI.

## Configuração e gateway

Veja [Configuração](docs/pt-BR/configuration.md) para o schema completo e a
precedência.
O gateway é independente do localhost automático: ele continua opcional e
serve para acesso a partir do navegador do host. Com gateway habilitado, uma
rota `frontend` da box `a4f8c9137d2b` é acessada em
`http://frontend.a4f8c9137d2b.localhost:<porta-gateway>`. O proxy preserva
WebSockets e cabeçalhos encaminhados.

Orientações específicas estão em [Windows](docs/pt-BR/windows.md). O plano, as
decisões e a auditoria de aceite ficam em
[wktbox-plano-tecnico.md](wktbox-plano-tecnico.md).
