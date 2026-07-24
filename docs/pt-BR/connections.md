# Conexões entre boxes

[English canonical guide](../connections.md).

Por padrão, cada box é isolada. Quando workloads de duas ou mais boxes prontas
precisarem conversar, crie uma conexão explícita:

```bash
wktbox list
wktbox connect a4f dfe --name dev-stack
```

Cada seletor pode ser o ID completo, um prefixo único com pelo menos três
caracteres ou o nome único da box. O comando se chama `connect` porque cria
conectividade bidirecional; `bind` normalmente sugere uma porta, endereço,
montagem ou vínculo unilateral.

## Acessar um serviço

Cada membro recebe um alias estável com o ID completo:

```text
<id-da-box>.wktbox
```

Se o Compose da box `dfe31c662a91` publicar `8000:3000`, um container de outro
membro acessa:

```text
http://dfe31c662a91.wktbox:8000
```

A porta publicada à esquerda em `ports:` é a porta alcançável. `EXPOSE` sozinho
e portas não publicadas não são expostos pela conexão. O Wktbox não reescreve o
Compose do projeto.

Os aliases usam IDs imutáveis, não nomes que podem mudar ou colidir. Os daemons,
imagens, volumes e redes internas dos projetos continuam separados. O socket
Docker do host não é montado.

## Visualizar e remover

```bash
wktbox connections
wktbox connections dev-stack
wktbox --json connections dev-stack
wktbox disconnect dev-stack
```

O estado pode ser `ready`, `degraded` ou `error`. `wktbox stop` preserva a
definição; `wktbox up` e `wktbox restart` refazem os vínculos necessários.
`disconnect` remove a ponte compartilhada sem parar as boxes.

Conecte somente boxes de desenvolvimento confiáveis. A conexão permite que
processos tentem alcançar listeners dos outros membros. A API de cada DinD
continua protegida por sua autoridade TLS própria, mas a rede compartilhada não
é uma fronteira para código hostil. Consulte [Segurança](security.md).

