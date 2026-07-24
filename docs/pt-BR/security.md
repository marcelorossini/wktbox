# Modelo de segurança

[English canonical guide](../security.md).

Wktbox oferece isolamento operacional entre worktrees confiáveis. Ele separa o
daemon Docker, containers, redes, imagens, caches e volumes de cada box. Não é
uma fronteira para executar código hostil.

## Propriedades

- cada box tem um DinD próprio;
- o socket `/var/run/docker.sock` do host não é montado no Webtop nem no DinD;
- a API do DinD usa TLS interno em `docker:2376` e não é publicada no host;
- portas externas próprias do Webtop e gateway são vinculadas a `127.0.0.1`;
- rotas automáticas de aplicações são vinculadas somente a `127.0.0.1` e
  `::1` dentro do namespace de rede do Webtop, nunca no host;
- o sidecar consulta a API DinD com os certificados TLS read-only e usa um
  socket Unix privado, `/run/wktbox-loopback/control.sock`, modo `0600`;
- o project env é montado como somente leitura;
- arquivos externos gerados usam diretório `0700` e arquivos `0600`;
- o estado guarda caminhos e metadados, não o conteúdo do project env;
- argumentos `--env` são redigidos em diagnósticos estruturados.
- publicações explícitas escutam somente no loopback do host;
- importações usam um relay autenticado por token aleatório de 256 bits, nunca
  exibido na saída humana ou JSON.

## Limites e riscos

O DinD usa `privileged: true`. A worktree é montada com escrita no Webtop e no
DinD. Processos da box podem ler e alterar arquivos do projeto e podem ler o
project env, mesmo que não consigam gravá-lo. Um Compose interno também controla
o daemon exclusivo e pode acessar tudo que estiver montado nele.

Uma porta TCP publicada no DinD fica acessível a qualquer processo que
compartilhe o namespace do Webtop. Isso é intencional para desenvolvimento, mas
não adiciona autenticação à aplicação. UDP não é encaminhado. Conflitos de bind
são mostrados no status e não fazem fallback para interfaces externas.

O Webtop escuta somente em loopback, mas a imagem não configura autenticação
própria do Wktbox. Qualquer processo ou usuário capaz de acessar o loopback do
host pode tentar abrir a porta atribuída. O gateway tem a mesma fronteira e
encaminha hosts configurados sem autenticação.

`git.mode: mounted` adiciona uma montagem com escrita dos metadados Git comuns
ao Webtop. Isso permite Git dentro da box, mas amplia o impacto de comandos,
hooks e ferramentas sobre o repositório. O modo padrão `host` evita essa
montagem.

TLS protege a API DinD contra acesso acidental fora dos containers da box. Nem
o Webtop nem o sidecar montam `/var/run/docker.sock` do host. Isso não
transforma um container privilegiado em VM. Limites declarados em `resources`
ainda não são aplicados no schema v1.

### Encaminhamento explícito

`wktbox port publish` torna deliberadamente uma porta da box acessível a
processos locais pelas interfaces `127.0.0.1` ou `::1`; ele não adiciona
autenticação à aplicação.

`wktbox port import` usa um relay nativo no host porque o loopback do container
não alcança diretamente o loopback do host real. O relay rejeita pedidos sem o
token de 256 bits antes de abrir o serviço de destino. O sidecar recebe
`SYS_ADMIN` para executar `setns` e `SYS_PTRACE` para acessar os handles de
namespace dos workloads. Em hosts com AppArmor, ele também usa
`apparmor=unconfined`, pois o perfil padrão do Docker bloqueia a entrada entre
containers. Essas permissões ficam restritas ao sidecar confiável, mas ampliam
materialmente sua autoridade dentro da box. O host Docker socket continua
ausente, e o token não é montado nos workloads. Essa é uma ampliação
operacional para código de desenvolvimento confiável, não uma fronteira contra
código hostil.

### Boxes conectadas

`wktbox connect` cria intencionalmente uma ponte compartilhada entre os
endpoints DinD e Webtop selecionados. Containers podem resolver os aliases dos
outros membros e alcançar portas publicadas. Daemons, imagens, volumes e redes
internas dos projetos continuam separados.

Considere todos os membros como parte da mesma rede de desenvolvimento
confiável: um processo pode tentar acessar qualquer listener disponível no
endpoint de outra box. Cada API DinD ainda usa uma autoridade TLS separada e o
socket Docker do host continua ausente, mas a conexão não isola código hostil.
Remova vínculos sem uso com `wktbox disconnect`.

## Segredos

Prefira um arquivo externo por worktree e `--env-file`. Não versione
`.wktbox.local.yml`, project env ou artefatos sensíveis. Não inclua segredos em
nomes de arquivos, argumentos de comando ou valores que a aplicação interna
imprima. Para ameaças entre usuários, código de terceiros ou workloads hostis,
use uma VM ou outro backend com uma fronteira de segurança dedicada.
