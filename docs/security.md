# Modelo de segurança

Wktbox oferece isolamento operacional entre worktrees confiáveis. Ele separa o
daemon Docker, containers, redes, imagens, caches e volumes de cada box. Não é
uma fronteira para executar código hostil.

## Propriedades

- cada box tem um DinD próprio;
- o socket `/var/run/docker.sock` do host não é montado no Webtop nem no DinD;
- a API do DinD usa TLS interno em `docker:2376` e não é publicada no host;
- portas externas são vinculadas a `127.0.0.1`;
- o project env é montado como somente leitura;
- arquivos externos gerados usam diretório `0700` e arquivos `0600`;
- o estado guarda caminhos e metadados, não o conteúdo do project env;
- argumentos `--env` são redigidos em diagnósticos estruturados.

## Limites e riscos

O DinD usa `privileged: true`. A worktree é montada com escrita no Webtop e no
DinD. Processos da box podem ler e alterar arquivos do projeto e podem ler o
project env, mesmo que não consigam gravá-lo. Um Compose interno também controla
o daemon exclusivo e pode acessar tudo que estiver montado nele.

O Webtop escuta somente em loopback, mas a imagem não configura autenticação
própria do Wktbox. Qualquer processo ou usuário capaz de acessar o loopback do
host pode tentar abrir a porta atribuída. O gateway tem a mesma fronteira e
encaminha hosts configurados sem autenticação.

`git.mode: mounted` adiciona uma montagem com escrita dos metadados Git comuns
ao Webtop. Isso permite Git dentro da box, mas amplia o impacto de comandos,
hooks e ferramentas sobre o repositório. O modo padrão `host` evita essa
montagem.

TLS protege a API DinD contra acesso acidental fora dos containers da box; não
transforma um container privilegiado em VM. Limites declarados em `resources`
ainda não são aplicados no schema v1.

## Segredos

Prefira um arquivo externo por worktree e `--env-file`. Não versione
`.wktbox.local.yml`, project env ou artefatos sensíveis. Não inclua segredos em
nomes de arquivos, argumentos de comando ou valores que a aplicação interna
imprima. Para ameaças entre usuários, código de terceiros ou workloads hostis,
use uma VM ou outro backend com uma fronteira de segurança dedicada.
