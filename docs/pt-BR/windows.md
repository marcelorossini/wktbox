# Windows e Docker Desktop

[English canonical guide](../windows.md).

O binário é compilado para Windows `amd64` e `arm64`. O runtime esperado é
Docker Desktop com containers Linux, Docker Compose v2 e uma unidade local
compartilhada com o Docker.

Antes de criar a primeira box:

```powershell
wktbox doctor --path "C:\worktrees\feature auth"
```

Caminhos locais com espaços são preservados pela descoberta, normalização,
geração de argumentos e build cruzado. Caminhos UNC (`\\servidor\share`) são
rejeitados explicitamente no MVP; use uma letra de unidade local compartilhada.
O teste runtime deste repositório foi executado em Linux, portanto Docker
Desktop, NTFS, bind mounts aninhados, watch de arquivos e desempenho ainda
precisam ser validados no host Windows real.

O localhost automático depende do namespace de rede Linux compartilhado por
`network_mode: service:webtop`. No Windows, isso roda dentro do backend de
containers Linux do Docker Desktop; não usa o namespace de rede nativo do
Windows. Assim, `localhost:5173` significa o loopback visto pelo navegador e
pelos processos dentro do Webtop. O acesso pelo navegador do host continua
pelas portas próprias do Webtop ou pelo gateway opcional.

No Windows, o Webtop usa `PUID=1000` e `PGID=1000`. Isso não garante que toda
imagem do Compose interno produza permissões convenientes no host. Dependências
pesadas, caches e bancos devem preferir volumes Docker internos em vez da
worktree.

O modo Git padrão é `git.mode: host`. Ele evita montar o diretório Git comum da
linked worktree no desktop Linux. `git.mode: mounted` é experimental no Windows:
hooks, symlinks, case-insensitivity e permissões precisam de validação específica
antes de uso.

Se `doctor` falhar:

- confirme que Docker Desktop está em containers Linux;
- confirme `docker version` e `docker compose version`;
- compartilhe a unidade que contém a worktree e o project env;
- habilite containers privilegiados conforme a política da máquina;
- verifique portas de loopback, o status do sidecar e espaço livre;
- evite UNC e caminhos não acessíveis ao Docker Desktop.

O modelo continua sendo de isolamento operacional, com DinD privilegiado e
worktree gravável. Consulte [Segurança](security.md).
