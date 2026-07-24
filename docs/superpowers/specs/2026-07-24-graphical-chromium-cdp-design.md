# Chromium gráfico com CDP por box

**Data:** 2026-07-24

## Objetivo

Toda box pronta deve manter aberto o Chromium gráfico do Webtop e expor seu
Chrome DevTools Protocol (CDP) em uma porta alta exclusiva no loopback do host.
O mesmo navegador visível no desktop deve ser controlável por clientes como
`chrome-devtools-mcp`, preservando janelas, cookies e perfil entre reinícios da
box.

Se o usuário fechar o Chromium, ele deve ser reaberto automaticamente. A
funcionalidade será padrão e obrigatória no schema v1, sem uma opção para
desativá-la.

## Abordagens consideradas

### 1. Supervisor da sessão gráfica no autostart do XFCE

Esta é a abordagem escolhida. Um processo iniciado pelo autostart do XFCE
executa o wrapper do Chromium em um laço. Ele herda `DISPLAY`, D-Bus e o
ambiente real da sessão gráfica. Quando a última janela fecha ou o Chromium
termina por falha, o processo aguarda brevemente e o inicia novamente.

O launcher normal do desktop chama o mesmo wrapper. Como todas as invocações
usam o mesmo diretório de perfil, clicar no ícone abre uma janela no processo
já supervisionado, sem criar outro navegador.

### 2. Serviço s6 iniciando o Chromium diretamente

O s6 oferece supervisão mais rígida, mas o processo nasce fora da sessão XFCE e
não recebe naturalmente o D-Bus da sessão gráfica. Seria necessário descobrir
e reconstruir esse ambiente, aumentando o acoplamento com detalhes internos da
imagem LinuxServer Webtop.

### 3. Chromium headless em serviço separado

É simples de tornar saudável e reiniciável, mas não controla o mesmo navegador
visível e não compartilha suas janelas. Isso contradiz o requisito principal.

## Comportamento externo

Ao concluir `wktbox up`, `wktbox run` ou outra operação que garanta a prontidão
da box:

1. o desktop Webtop está em execução;
2. o Chromium gráfico está aberto;
3. `http://127.0.0.1:<porta-cdp>/json/version` responde;
4. o `webSocketDebuggerUrl` retornado é utilizável por um cliente no host;
5. fechar o Chromium faz uma nova instância gráfica surgir automaticamente;
6. cookies, preferências e demais dados do perfil sobrevivem a
   `stop`/`restart`.

`wktbox status` mostrará uma linha semelhante a:

```text
Browser CDP: http://localhost:23005
```

`wktbox --json status` incluirá:

```json
{
  "urls": {
    "browserCdp": "http://localhost:23005"
  },
  "ports": {
    "browserCdp": 23005
  }
}
```

## Alocação e publicação da porta

O Chromium escuta em `127.0.0.1:9222` dentro do namespace do Webtop. Como
Chromium moderno mantém o CDP restrito ao loopback mesmo quando recebe uma
flag de endereço remoto, um relay TCP supervisionado pelo s6 escuta em
`0.0.0.0:9223` no mesmo namespace e encaminha bytes para
`127.0.0.1:9222`. O Compose externo publica somente a porta do relay e somente
em `127.0.0.1` no host.

Cada box já reserva um bloco de dez portas altas. O CDP usará o offset `+5`:

| Offset | Uso |
|---:|---|
| `+0` | Webtop HTTP |
| `+1` | Webtop HTTPS |
| `+2` | SSH reservado |
| `+3` | Gateway |
| `+5` | Chromium CDP |

Assim, boxes iniciadas em `23000`, `23010` e `23020` terão CDP em `23005`,
`23014` e `23024`. O alocador continuará verificando o bloco completo antes de
reservá-lo.

O modelo `ports.Block` ganhará `BrowserCDP()`. A renderização fornecerá
`PORT_BROWSER_CDP` ao Compose externo e o serviço `webtop` declarará:

```yaml
ports:
  - 127.0.0.1:${PORT_BROWSER_CDP}:9223
```

## Inicialização e perfil do Chromium

A imagem Webtop fornecerá os seguintes artefatos próprios:

- um wrapper compatível com o wrapper atual da imagem LinuxServer;
- um supervisor de sessão que relança o wrapper após o encerramento;
- uma entrada XDG em `/etc/xdg/autostart`.
- um serviço s6 que supervisiona o relay `socat` entre `9223` e `9222`.

O wrapper sempre adicionará:

```text
--remote-debugging-port=9222
--user-data-dir=/config/.config/wktbox-chromium
```

O diretório explicitamente não padrão é necessário porque versões modernas do
Chromium ignoram a depuração remota quando ela usa o diretório de dados padrão.
Ele permanece sob o volume `webtop-config`, portanto é persistente. Locks
`Singleton*` obsoletos serão removidos somente quando nenhum processo Chromium
estiver em execução, preservando o comportamento do wrapper base.

O supervisor será um processo simples da sessão:

1. inicia o wrapper;
2. espera o Chromium terminar;
3. aguarda um intervalo curto para evitar um laço agressivo em falhas;
4. inicia novamente enquanto a sessão XFCE existir.

O encerramento do container ou da sessão deve encerrar o supervisor sem deixar
processos órfãos.

## Prontidão e falhas

O serviço `webtop` terá um healthcheck que consulta o caminho completo pelo
relay em `http://127.0.0.1:9223/json/version`. A avaliação de prontidão do
Wktbox exigirá o Webtop saudável, além dos serviços já exigidos.

Consequências:

- o primeiro start pode demorar alguns segundos a mais enquanto XFCE e Chromium
  iniciam;
- falha persistente do Chromium impede a box de ser reportada como pronta;
- fechar o Chromium pode produzir uma transição breve para `unhealthy`, seguida
  de recuperação após o relançamento;
- o timeout existente de prontidão continuará fornecendo o erro ao usuário e
  os logs do Webtop continuarão sendo a fonte de diagnóstico.

O healthcheck validará o endpoint, não somente a existência do processo. Isso
prova que o cliente externo pode começar a negociação CDP.

## Segurança

CDP não possui autenticação e concede controle total sobre o navegador,
incluindo páginas, cookies e armazenamento. A publicação no host será
estritamente `127.0.0.1`; nunca `0.0.0.0`.

Dentro do namespace Docker externo, o relay aceita conexões na interface do
container para que a publicação funcione, enquanto o Chromium permanece no
loopback. Isso é compatível com o modelo do Wktbox como isolamento de
conveniência, não como barreira para workloads hostis. A documentação de
segurança explicará o impacto.

Nenhuma porta do daemon DinD ou socket Docker do host será adicionada.

## Compatibilidade

Boxes existentes serão reconciliadas por `wktbox up` ou `wktbox restart`, que
atualizarão a imagem/Compose externo e publicarão o novo offset. O bloco atual
já tem tamanho dez, então nenhum ID ou início de bloco precisa mudar.

O novo perfil não migra automaticamente dados do diretório padrão
`/config/.config/chromium`. A migração implícita poderia copiar dados
incompletos de um navegador em execução. A partir da atualização, o perfil
`wktbox-chromium` será o perfil gráfico canônico e persistente.

Imagens Webtop customizadas continuam obrigadas a cumprir o contrato do runner.
Se não fornecerem o Chromium/CDP esperado, o healthcheck torna a
incompatibilidade explícita em vez de retornar uma box falsamente pronta.

## Testes

### Unidade e renderização

- `ports.Block.BrowserCDP()` retorna `Start + 5`;
- blocos menores que cinco portas são rejeitados;
- o ambiente renderizado contém `PORT_BROWSER_CDP`;
- o Compose externo publica `127.0.0.1:${PORT_BROWSER_CDP}:9223`;
- o relay s6 encaminha `0.0.0.0:9223` para `127.0.0.1:9222`;
- o Webtop contém o healthcheck de CDP;
- status humano e JSON expõem a URL e a porta;
- prontidão exige `webtop` saudável.

### Imagem

- o Dockerfile instala wrapper, supervisor e entrada de autostart;
- testes estáticos validam permissões e argumentos obrigatórios;
- o wrapper preserva argumentos adicionais recebidos pelo launcher.

### E2E

Uma box real deve provar:

1. `/json/version` responde na porta alta do host;
2. uma página no localhost isolado da box pode ser aberta via CDP;
3. a janela é visível no Chromium gráfico do Webtop;
4. encerrar o processo principal do Chromium faz o supervisor criar outro PID;
5. o endpoint CDP volta a responder;
6. duas boxes simultâneas usam portas CDP diferentes;
7. a porta não escuta em interfaces não loopback do host.

## Fora de escopo

- autenticação ou TLS para CDP;
- seleção dinâmica de outro navegador;
- múltiplos perfis por box;
- configuração para desativar o navegador no schema v1;
- alteração automática da configuração global do MCP do Codex;
- acesso CDP entre hosts.
