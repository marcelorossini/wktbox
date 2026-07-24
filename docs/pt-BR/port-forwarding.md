# Encaminhamento bidirecional de portas

[English canonical guide](../port-forwarding.md).

As portas normais do projeto continuam privadas a cada box. Use os comandos
explícitos `wktbox port` somente quando o tráfego precisar cruzar a fronteira
com o host real. A box deve existir. Para adicionar importações ou publicações,
ela precisa estar pronta; listar e remover mapeamentos também funcionam com a
box parada. O recurso encaminha somente TCP.

## Trazer uma porta do host para o localhost dos containers

Normalmente, `localhost` dentro de um container aponta para o próprio
container, não para o host real. O comando de importação cria explicitamente a
ponte:

```bash
wktbox port import 1234
```

O caminho fica:

```text
host real 127.0.0.1:1234 -> localhost:1234 de cada workload
```

Depois disso, `localhost:1234` e `127.0.0.1:1234` funcionam dentro de todos os
workloads em execução. Containers iniciados depois recebem a mesma importação.

Para remapear a porta:

```bash
wktbox port import \
  --map api=127.0.0.1:1234:4321
```

Nesse caso, o serviço do host em `1234` aparece em `localhost:4321` dentro dos
containers.

## Vários serviços ao mesmo tempo

Portas com o mesmo número nos dois lados:

```bash
wktbox port import 1234 5432 6379
```

Vários serviços nomeados e remapeados:

```bash
wktbox port import \
  --map api=127.0.0.1:1234:4321 \
  --map postgres=127.0.0.1:5432:5432 \
  --map redis=127.0.0.1:6379:16379
```

Não misture portas posicionais e `--map` no mesmo comando.

Antes de ativar, o Wktbox testa todas as portas em todos os workloads. Se algum
container já usa uma delas, o lote inteiro falha: nenhum proxy, arquivo ou
estado parcial é mantido. Se um container futuro iniciar usando uma porta
reservada, esse container é parado e `wktbox status` registra
`port_import_conflict`; os demais workloads continuam com a importação.

## Publicar uma porta da box no host

O sentido contrário usa outro comando. A porta da box é o lado esquerdo de uma
publicação do Compose interno. Para `8000:3000`:

```bash
wktbox port publish \
  --map api=127.0.0.1:18000:8000
```

O caminho fica:

```text
host real 127.0.0.1:18000 -> docker:8000 da box -> porta 3000 do workload
```

Para publicar vários serviços em uma única transação:

```bash
wktbox port publish \
  --map frontend=127.0.0.1:15173:5173 \
  --map api=127.0.0.1:18000:8000 \
  --map postgres=127.0.0.1:15432:5432
```

Publicações aceitam somente `127.0.0.1` ou `::1`. Se qualquer listener do host
já estiver ocupado, o lote inteiro é rejeitado e nada é alterado.

## Listar, remover e reiniciar

```bash
wktbox port list
wktbox --json port list

wktbox port remove api postgres
wktbox port remove --all-imports
wktbox port remove --all-publications
```

`wktbox stop` fecha os listeners, mas preserva as definições. `wktbox up` e
`wktbox restart` restauram os dois sentidos. `wktbox destroy` remove tudo junto
com a box. Os workloads internos continuam obedecendo à própria política de
restart do Docker; use uma política como `unless-stopped` quando um serviço
precisar voltar automaticamente com a box.

Com a box parada, `wktbox port list` mostra os mapeamentos como `stopped` e
`wktbox port remove` altera apenas a configuração desejada, sem iniciar a box.
Com a box ativa, o estado observado verifica o `portbridge`, o relay autenticado,
o fluxo de eventos e os processos de proxy de cada workload.

Nomes aceitam letras minúsculas, números, ponto, sublinhado e hífen. Eles são
únicos na box, mesmo entre importação e publicação. Toda alteração é atômica:
erro de sintaxe, nome duplicado, porta ocupada, falha de relay, Compose ou
persistência restaura o estado anterior.
Um lock compartilhado impede o reconciliador em segundo plano de ler uma
configuração candidata antes do commit. Se o próprio rollback do runtime
falhar, a box fica persistida como degradada, nunca como pronta. Um
`wktbox restart <box>` ou `wktbox up` bem-sucedido reconcilia o runtime e limpa
essa degradação.

Veja [Segurança](security.md) e o
[guia de diagnóstico](../troubleshooting.md).
