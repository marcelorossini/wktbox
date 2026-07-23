# Automatic Webtop Loopback Design

Status: aprovado em 23 de julho de 2026

## 1. Objetivo

O Wktbox deve espelhar automaticamente, dentro do namespace de rede do
Webtop, todas as portas TCP efetivamente publicadas por containers em execução
no daemon DinD exclusivo da box.

Para uma publicação Docker interna:

```text
HOST_PORT:CONTAINER_PORT
```

o encaminhamento será:

```text
127.0.0.1:HOST_PORT -> docker:HOST_PORT
[::1]:HOST_PORT     -> docker:HOST_PORT
```

O destino é sempre a porta publicada no namespace do DinD, nunca a porta
privada do container.

O fluxo mínimo deve funcionar sem `.wktbox.yml`:

```bash
wktbox run -- docker compose up -d
wktbox exec -- curl -I http://localhost:5173
```

Nenhuma rota, porta, endereço IP ou alteração no Compose do projeto será
exigida.

## 2. Decisões de arquitetura

### 2.1 Sidecar no namespace do Webtop

O Compose externo ganhará um serviço obrigatório chamado `loopback`. Ele usará
a mesma imagem do Webtop e executará apenas o binário
`/usr/local/bin/wktbox-loopback`.

O serviço declarará:

```yaml
network_mode: service:webtop
```

Assim, o sidecar e o desktop compartilham a mesma interface `lo`. Listeners
abertos pelo sidecar em `127.0.0.1` e `::1` são os mesmos endereços observados
pelo terminal e pelo Chromium do Webtop.

O sidecar separado foi escolhido em vez de:

- integrar um serviço ao `s6` da imagem LinuxServer, o que acoplaria o Wktbox à
  implementação interna da imagem;
- criar processos `socat` por porta a partir de scripts, o que tornaria
  reconexão, sinais, status e idempotência mais frágeis.

O sidecar não publica porta alguma no host.

### 2.2 Binário Go dedicado

Será criado `cmd/wktbox-loopback`, apoiado por pacotes pequenos em
`internal/loopback`.

O processo usará os módulos oficiais `github.com/moby/moby/client` e
`github.com/moby/moby/api`, com negociação automática da versão da Engine API.
Ele consumirá:

```text
DOCKER_HOST=tcp://docker:2376
DOCKER_TLS_VERIFY=1
DOCKER_CERT_PATH=/certs/client
```

Nenhum socket Docker do host será montado.

### 2.3 Imagem única

O Dockerfile do Webtop ganhará um estágio Go que compila
`wktbox-loopback` estaticamente para Linux. O contexto de build passará a ser a
raiz do repositório:

```bash
docker build -f images/webtop/Dockerfile -t wktbox/webtop:dev .
```

O sidecar reutiliza essa imagem; não haverá uma terceira imagem publicada.

### 2.4 Gateway existente

O gateway Nginx atual permanece opcional e separado para acesso pelo navegador
do host. Ele não participa do loopback automático.

`gateway.routes` continua aceito por compatibilidade, mas não é necessário para
acessar as aplicações dentro do Webtop e deixa de aparecer no fluxo principal
da documentação.

## 3. Portas próprias do Webtop

As portas internas padrão do LinuxServer Webtop colidem com portas comuns de
aplicações. O Compose externo passará a usar:

```text
CUSTOM_PORT=61000
CUSTOM_HTTPS_PORT=61001
CUSTOM_WS_PORT=61002
```

Os bindings do host serão ajustados para:

```text
127.0.0.1:${PORT_HTTP}:61000
127.0.0.1:${PORT_HTTPS}:61001
```

O binding externo `${PORT_SSH}:22` será removido porque SSH está desabilitado e
rejeitado pelo schema v1. O offset continua reservado no bloco de portas para
compatibilidade do allocator, mas não expõe serviço algum.

Essa remoção também impede que uma eventual rota automática `localhost:22`
seja encaminhada indiretamente ao host pela antiga publicação do Webtop.

As portas 61000, 61001 e 61002 permanecem ocupadas pelos serviços do Webtop.
Se o Compose interno publicar uma delas, o loopback registrará conflito sem
derrubar a box.

## 4. Descoberta de publicações

### 4.1 Fonte de verdade

O processo lista somente containers em execução e executa inspect em cada um.
A fonte de verdade é:

```text
NetworkSettings.Ports
```

Uma rota desejada existe somente quando há um `PortBinding` com `HostPort`
numérico entre 1 e 65535.

Não serão consideradas:

- entradas de `Config.ExposedPorts` sem binding;
- `EXPOSE` do Dockerfile;
- `expose:` do Compose;
- portas documentadas por imagem;
- varredura de rede;
- containers parados.

### 4.2 Porta publicada

Para:

```text
"8000:3000"
```

o inspect informa a chave privada `3000/tcp`, mas o binding contém
`HostPort=8000`. A rota desejada é:

```text
localhost:8000 -> docker:8000
```

A chave privada não é usada como destino.

Bindings IPv4 e IPv6 da mesma publicação são deduplicados pela tupla
`HostPort/protocolo`. Se mais de um registro descrever a mesma porta, uma única
rota é mantida.

O campo `HostIp` é preservado apenas como metadado diagnóstico. Conforme o
modelo de rede aprovado, novas conexões sempre usam `docker:HostPort`.

### 4.3 Origem

O nome de origem apresentado segue esta ordem:

1. label `com.docker.compose.service`, quando presente;
2. nome do container sem a barra inicial;
3. ID curto do container.

Se mais de um binding resultar na mesma rota, as origens são agregadas e
ordenadas para manter status determinístico.

### 4.4 UDP

Publicações UDP não criam listeners. Elas aparecem em warnings:

```text
UDP loopback mirroring is not supported yet.
```

O warning identifica porta e origem, sem impedir as rotas TCP.

## 5. Reconciliador de listeners

### 5.1 Estado desejado e estado real

Cada full resync produz um mapa imutável:

```text
published TCP port -> route metadata
```

O reconciliador compara esse mapa com os listeners atuais:

- rotas idênticas permanecem abertas;
- rotas novas tentam abrir os dois endereços loopback;
- rotas removidas fecham ambos os listeners;
- mudanças apenas de origem atualizam metadados sem recriar sockets;
- nenhuma porta possui mais de um par de listeners.

### 5.2 Bind atômico

Uma rota só recebe status `listening` quando ambos os binds funcionam:

```text
127.0.0.1:PORT
[::1]:PORT
```

Se um bind falhar, qualquer socket aberto pela tentativa é fechado e a rota
recebe status `conflict`.

Conflito não altera a prontidão da box e não bloqueia outras portas.

### 5.3 Proxy TCP

Cada listener aceita conexões concorrentes. Para cada conexão:

1. resolve o hostname `docker`;
2. abre `docker:PORT` com timeout de 3 segundos;
3. copia bytes nas duas direções;
4. propaga half-close quando suportado;
5. fecha os dois lados ao terminar.

O proxy é agnóstico ao protocolo. HTTP, HTTPS, WebSocket, hot reload e bancos
funcionam como fluxos TCP transparentes.

O IP resolvido não é armazenado. Uma conexão nova sempre faz nova resolução,
permitindo recuperar-se quando o container DinD for recriado com outro IP.

Falha no upstream não remove o listener. Ela é temporária e afeta somente a
conexão corrente.

### 5.4 Sinais

O PID 1 do sidecar trata `SIGTERM` e `SIGINT`:

- cancela o stream de eventos;
- fecha o socket de controle;
- fecha todos os listeners;
- aguarda accept loops e cópias ativas por no máximo 10 segundos;
- sai sem processos órfãos.

## 6. Eventos, backoff e ressincronização

O daemon:

1. conecta ao stream de eventos do Docker interno;
2. inicia um full resync;
3. observa eventos de container `start`, `die`, `stop`, `destroy` e `rename`;
4. agrupa rajadas curtas com debounce de 100 milissegundos;
5. executa full resync em cada grupo relevante.

Ao perder a conexão:

- marca o stream como desconectado no status;
- aplica backoff exponencial iniciado em 250 milissegundos, dobrado a cada
  tentativa até o máximo de 5 segundos, com jitter de ±20%;
- resolve `docker` novamente em cada tentativa;
- reabre o stream;
- executa full resync obrigatório após reconectar;
- zera o backoff depois de uma conexão estável.

Um resync periódico a cada 30 segundos funciona como defesa adicional contra
eventos perdidos. Ele não substitui o stream.

Logs normais registram somente início, mudança de topologia, reconexão e
shutdown. Erros repetitivos de conexão são limitados a uma emissão a cada 30
segundos por classe de erro.

## 7. Controle e consistência após comandos

O sidecar expõe um socket Unix privado dentro do próprio container:

```text
/run/wktbox-loopback/control.sock
```

O mesmo binário oferece:

```text
wktbox-loopback serve
wktbox-loopback sync --json
wktbox-loopback status --json
```

`sync` solicita um full resync síncrono ao processo principal e só retorna
depois que listeners e status foram atualizados.

Depois de um comando filho bem-sucedido em `wktbox run` ou
`wktbox compose`, a CLI do host executa `sync` por:

```text
docker compose ... exec -T loopback wktbox-loopback sync --json
```

Isso elimina a corrida entre `docker compose up -d` e o primeiro acesso.

Se o comando filho falhar, seu exit code continua prioritário e não é
substituído por uma tentativa de sync.

Containers criados depois por terminal, `wktbox exec` ou outras ferramentas
continuam cobertos pelo stream de eventos e pelo resync periódico.

## 8. Estado e prontidão

O serviço `loopback` terá healthcheck baseado no socket de controle. Uma box só
fica `ready` quando:

- DinD está running e healthy;
- Webtop está running;
- loopback está running e healthy.

Conflitos de portas e UDP não suportado não tornam o sidecar unhealthy.

O status dinâmico do loopback não é gravado em `state.json`. `BoxRecord` pode
transportá-lo transitoriamente com `json:"-"`, e `wktbox status` consulta o
sidecar em tempo real.

Perder `state.json` continua recuperável pelos labels do Compose externo,
incluindo a presença do serviço obrigatório `loopback`.

## 9. Modelo de status

O contrato lógico é:

```json
{
  "eventStream": "connected",
  "updatedAt": "2026-07-23T12:00:00Z",
  "routes": [
    {
      "port": 5173,
      "target": "docker:5173",
      "sources": ["frontend"],
      "protocol": "tcp",
      "status": "listening"
    }
  ],
  "conflicts": [
    {
      "port": 61000,
      "target": "docker:61000",
      "sources": ["conflicting-service"],
      "protocol": "tcp",
      "status": "conflict",
      "warning": "localhost:61000 is already used inside Webtop"
    }
  ],
  "warnings": [
    {
      "port": 5353,
      "protocol": "udp",
      "sources": ["discovery"],
      "message": "UDP loopback mirroring is not supported yet."
    }
  ]
}
```

Arquivos internos de status são escritos atomicamente e nunca contêm
variáveis de ambiente, certificados ou dados do project env.

## 10. Saída da CLI

### 10.1 `wktbox status`

A saída humana acrescenta:

```text
Automatic Webtop loopback:
  localhost:5173 -> docker:5173
    source: frontend
    protocol: tcp
    status: listening

  localhost:61000 -> docker:61000
    source: conflicting-service
    protocol: tcp
    status: conflict
    warning: localhost:61000 is already used inside Webtop
```

O JSON acrescenta um campo `loopback` com o contrato acima.

Se a box estiver parada, o campo indica `unavailable` sem transformar o status
da box em erro adicional.

### 10.2 `run` e `compose`

Depois do sync, a saída diagnóstica vai para stderr para preservar stdout,
stderr e pipelines do processo filho:

```text
Automatic localhost routes inside Webtop:
  http://localhost:5173
  http://localhost:8000
  localhost:5432 -> docker:5432
```

O prefixo de conveniência é apenas apresentação. A classificação HTTP é usada
somente para `80`, `3000`, `4173`, `5000`, `5173`, `8000` e `8080`; HTTPS,
somente para `443` e `8443`; as demais portas aparecem como `tcp`. Toda linha
deriva do mesmo proxy TCP e o status sempre declara `protocol: tcp`.

`--quiet` suprime o resumo. Com `--json`, o diagnóstico em stderr é um objeto
JSON; stdout do filho permanece intocado.

## 11. Ciclo de vida

- `up`: cria Webtop e loopback; o sidecar reconcilia containers existentes.
- `run`/`compose`: executam o comando, solicitam sync e mostram rotas.
- eventos posteriores: criam ou removem rotas sem intervenção da CLI.
- Compose interno `down/up`: listeners somem e reaparecem automaticamente.
- restart do daemon interno: o stream reconecta e executa full resync.
- recriação do DinD com IP diferente: novas conexões resolvem `docker`.
- restart do Webtop: o sidecar compartilha novamente seu namespace.
- `stop`: Compose externo encerra sidecar, Webtop e DinD.
- `destroy`: remove sidecar junto com a box, sem processo órfão.

## 12. Segurança e isolamento

- nenhum socket Docker do host é montado;
- o sidecar acessa somente o daemon interno por TLS;
- listeners usam somente `127.0.0.1` e `::1` no namespace do Webtop;
- o serviço `loopback` não declara `ports`;
- nenhuma porta da aplicação interna é publicada pelo Compose externo;
- certificados são montados read-only;
- status e logs não incluem segredos;
- o mecanismo continua sendo isolamento operacional, não sandbox para código
  hostil.

## 13. Estratégia de testes

### 13.1 Unidade

Os testes de descoberta cobrem:

- binding `5173:5173`;
- binding `8000:3000` usando 8000 como destino;
- deduplicação IPv4/IPv6;
- containers parados;
- EXPOSE sem publicação;
- TCP e UDP;
- portas inválidas;
- nomes e ordenação determinística.

Os testes do reconciliador usam listeners reais em loopback para cobrir:

- criação;
- manutenção idempotente;
- remoção;
- conflito;
- nenhuma duplicação;
- bind IPv4 e IPv6;
- shutdown.

Os testes do proxy usam servidores TCP locais para validar:

- HTTP;
- HTTPS;
- WebSocket bidirecional;
- conexões concorrentes;
- resolução do destino por conexão;
- half-close.

Os testes do daemon e controle cobrem:

- full resync inicial;
- eventos relevantes;
- debounce;
- reconexão com backoff;
- full resync após reconectar;
- sync síncrono;
- status atômico;
- SIGTERM.

### 13.2 Integração do Compose externo

Os testes de renderização e Compose cobrem:

- serviço `loopback` obrigatório;
- `network_mode: service:webtop`;
- TLS e certs read-only;
- ausência de `ports` no sidecar;
- Webtop em 61000/61001/61002;
- ausência do binding SSH;
- healthcheck e prontidão;
- build da imagem com o binário.

### 13.3 E2E obrigatório

Um teste real sem `gateway.routes` ou configuração de loopback provará:

| # | Cenário | Evidência |
|---:|---|---|
| 1 | box sem configuração adicional | não existe `.wktbox.yml` de rotas |
| 2 | serviço `5173:5173` | inspect interno mostra publicação |
| 3 | HTTP automático | `wktbox exec -- curl -I http://localhost:5173` retorna 200 |
| 4 | `8000:3000` | status mostra `localhost:8000 -> docker:8000` e HTTP responde |
| 5 | container criado depois | polling observa rota criada pelo evento |
| 6 | container removido | status perde rota e conexão local falha |
| 7 | DinD com novo IP | IP anterior é ocupado, DinD recriado recebe outro e proxy recupera |
| 8 | duas boxes | ambas usam `localhost:5173` simultaneamente |
| 9 | WebSocket/hot reload | conexão upgrade bidirecional permanece funcional |
| 10 | restart do daemon | status mostra stream reconectado e rota volta |
| 11 | conflito Webtop | publicação 61000 aparece como `conflict`; outras continuam |
| 12 | nada no host | inspect externo não publica 5173, 8000 ou portas dinâmicas da aplicação |
| 13 | sem socket host | inspect externo confirma ausência de `/var/run/docker.sock` |

Além da matriz, Chromium headless dentro do Webtop abrirá
`http://localhost:5173` e validará o conteúdo da página.

## 14. Critérios de conclusão

A feature só estará concluída quando:

1. todos os testes unitários e de integração passarem com race detector;
2. os treze cenários E2E tiverem evidência real;
3. Chromium e curl funcionarem dentro do Webtop;
4. duas boxes usarem a mesma porta local sem colisão;
5. restart e recriação com IP diferente forem validados;
6. status humano e JSON mostrarem rotas, conflitos e UDP;
7. run/compose mostrarem o resumo sem corromper stdout do filho;
8. nenhum socket do host ou porta de aplicação for exposto;
9. builds Linux, Windows e macOS do binário principal continuarem funcionando;
10. README, configuração, segurança, Windows e plano técnico refletirem o novo
    comportamento.

## 15. Referências

- Docker Engine API:
  https://docs.docker.com/reference/api/engine/
- Docker Compose `network_mode: service:<name>`:
  https://docs.docker.com/reference/compose-file/services/
- LinuxServer Webtop e portas customizadas:
  https://docs.linuxserver.io/images/docker-webtop/
