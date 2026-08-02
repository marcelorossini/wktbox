# Ferramentas de desenvolvimento no Webtop

## Objetivo

Transformar o Webtop em um ambiente de desenvolvimento mais completo sem
abandonar o modelo de imagem imutável do Wktbox. Toda imagem publicada deve
incluir NVM, uma versão LTS do Node.js, Python, Visual Studio Code e DBeaver
Community, tanto em `linux/amd64` quanto em `linux/arm64`.

Além das ferramentas solicitadas, a imagem incluirá um conjunto pequeno de
utilitários necessários no uso diário e na compilação de dependências:
`build-essential`, `jq`, `ripgrep`, `unzip` e `zip`.

## Abordagens consideradas

### 1. Ferramentas incorporadas à imagem — escolhida

O Dockerfile instala as ferramentas durante o build. A imagem resultante é
autossuficiente, os builds falham cedo quando uma fonte deixa de funcionar e
uma recriação da box preserva somente os dados do usuário, não instalações
imperativas difíceis de reproduzir.

O custo é o aumento do tamanho e do tempo de build, principalmente por causa
de VS Code, DBeaver e Node.js. Esse custo é coerente com o propósito do Webtop
como ambiente gráfico de desenvolvimento.

### 2. Instalação no primeiro start

Um script poderia baixar todas as ferramentas quando o volume `/config` fosse
criado. Isso reduziria a imagem, mas tornaria a primeira inicialização lenta e
dependente de rede, além de permitir que duas boxes criadas com a mesma imagem
recebessem versões diferentes.

### 3. PRoot Apps ou Flatpak para aplicativos gráficos

Essa opção isolaria VS Code e DBeaver do sistema base, mas adicionaria outro
gerenciador de aplicações, mais armazenamento e pontos de integração com o
desktop. Também deixaria a validação multi-arquitetura menos direta.

## Arquitetura

### Pacotes do Ubuntu

O estágio final continuará baseado em
`lscr.io/linuxserver/webtop:ubuntu-xfce`. A instalação APT existente será
estendida com:

- `python3`, `python3-pip`, `python3-venv`, `python-is-python3` e `pipx`;
- `build-essential`, `jq`, `ripgrep`, `unzip` e `zip`;
- dependências mínimas para cadastrar repositórios assinados.

`python` e `python3` apontarão para o mesmo Python fornecido pelo Ubuntu. O
ambiente global não receberá bibliotecas Python adicionais; projetos deverão
usar `venv`, e ferramentas de linha de comando poderão usar `pipx`.

### NVM e Node.js

O NVM será obtido do repositório oficial em uma versão declarada por `ARG` no
Dockerfile. Durante o build, ele instalará a linha LTS ativa do Node.js e a
definirá como padrão. Isso entrega `node`, `npm`, `npx` e `corepack` já na
primeira sessão, sem impedir a instalação de outras versões.

A cópia imutável construída na imagem servirá de semente. Antes de iniciar o
LinuxServer `/init`, o entrypoint inicializará `/config/.nvm` somente quando o
diretório ainda não existir. O conteúdo persistente nunca será substituído em
starts posteriores. O diretório receberá o `PUID` e `PGID` configurados para
que o usuário `abc` possa executar `nvm install`, `nvm alias` e remover versões.

Um arquivo comum de inicialização do shell exportará `NVM_DIR=/config/.nvm`,
carregará `nvm.sh` quando disponível e selecionará silenciosamente a versão
padrão. A configuração deverá funcionar em terminais Bash gráficos e em
comandos Bash de verificação executados pelo entrypoint.

### Visual Studio Code

O Dockerfile cadastrará a chave e o repositório APT oficiais da Microsoft em
arquivos `Signed-By`, sem usar `apt-key`. A definição do repositório limitará
explicitamente as arquiteturas a `amd64` e `arm64`, que são as plataformas
publicadas pelo Wktbox, e instalará o pacote estável `code`.

Extensões e preferências permanecerão sob o diretório home do usuário, que no
Webtop é `/config`. A imagem não instalará extensões por padrão; projetos
diferentes não devem herdar escolhas opinativas ou credenciais de extensões.

### DBeaver Community

O Dockerfile cadastrará a chave e o repositório Debian oficiais do DBeaver e
instalará `dbeaver-ce`. O repositório contém pacotes para `amd64` e `arm64` e a
distribuição inclui o Java necessário, portanto nenhum JDK separado será
adicionado.

Conexões, drivers baixados e preferências do DBeaver ficam no home persistente
`/config`. Nenhuma conexão de banco ou driver adicional será pré-configurado.

### Persistência e inicialização

O volume `webtop-config` já montado em `/config` continuará sendo o único
estado persistente das ferramentas. O entrypoint realizará apenas a
inicialização idempotente do NVM e depois manterá o comportamento existente:
executará o comando recebido ou delegará para `/init`.

Falhas ao criar a semente do NVM serão fatais, com mensagem no log do
container. Um volume que já possua `/config/.nvm` será considerado propriedade
do usuário e não será migrado ou sobrescrito automaticamente.

## Validação

Um novo teste de imagem deverá:

1. falhar antes da implementação ao não encontrar o contrato das ferramentas;
2. construir a imagem real do Webtop;
3. iniciar um container com `/config` vazio pelo entrypoint;
4. comprovar a disponibilidade e as versões de `nvm`, `node`, `npm`, `python`,
   `python3`, `pip`, `pipx`, `code` e do pacote `dbeaver-ce`;
5. comprovar que a semente NVM é gravável pelo usuário configurado;
6. iniciar novamente com o mesmo `/config` e comprovar que um marcador criado
   pelo usuário dentro de `.nvm` não foi sobrescrito;
7. validar que os pacotes adicionais estão presentes;
8. executar os testes de imagem existentes e o conjunto completo do projeto.

O fluxo de release já constrói o Webtop para `linux/amd64` e `linux/arm64`.
Além do smoke test local, a configuração dos repositórios e downloads deverá
ser independente de arquitetura, sem nomes de artefato fixados em `amd64`.

## Documentação

O README em português e em inglês ganhará uma seção curta com o inventário do
ambiente incluído. Ela explicará que configurações e versões adicionais do NVM
persistem em `/config`, enquanto atualizações das ferramentas fornecidas pela
imagem chegam por uma nova imagem do Wktbox.

## Fora de escopo

- instalar extensões do VS Code;
- pré-configurar conexões ou credenciais do DBeaver;
- adicionar `pyenv`, Conda ou bibliotecas Python globais;
- atualizar aplicações automaticamente durante o start;
- alterar portas, permissões ou o modelo de isolamento da box.
