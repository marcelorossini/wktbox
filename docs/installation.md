# Installation, updates, and removal

Official releases provide checksum-verified installers and archives for Linux,
macOS, and Windows on amd64 and arm64.

## Quick install

Linux and macOS:

```bash
curl -fsSL \
  https://github.com/marcelorossini/wktbox/releases/latest/download/install.sh \
  | sh
```

Windows PowerShell:

```powershell
irm https://github.com/marcelorossini/wktbox/releases/latest/download/install.ps1 |
  iex
```

The Unix installer defaults to `$HOME/.local/bin/wktbox`. The Windows installer
defaults to `%LOCALAPPDATA%\Programs\Wktbox\bin\wktbox.exe`. Both download
`checksums.txt`, verify the selected archive, and replace the executable
atomically.

## Download, inspect, and run

Piping a trusted installer is concise; downloading it first gives you an
inspection point.

Linux or macOS:

```bash
curl -fLO \
  https://github.com/marcelorossini/wktbox/releases/latest/download/install.sh
less install.sh
sh install.sh
```

Windows PowerShell:

```powershell
Invoke-WebRequest `
  https://github.com/marcelorossini/wktbox/releases/latest/download/install.ps1 `
  -OutFile install.ps1
Get-Content .\install.ps1
& .\install.ps1
```

## Install a fixed version

Use a fixed version for reproducible developer images or managed fleets.

Linux or macOS:

```bash
sh install.sh --version 0.1.0
```

Windows PowerShell:

```powershell
& .\install.ps1 -Version 0.1.0
```

Custom destinations are supported:

```bash
sh install.sh --version 0.1.0 --install-dir "$HOME/bin"
```

```powershell
& .\install.ps1 -Version 0.1.0 -InstallDir "$HOME\bin"
```

## PATH

If the installer reports that its directory is absent from `PATH`, add it to
your shell profile.

Linux or macOS:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

Windows PowerShell, for the current user:

```powershell
$dir = "$env:LOCALAPPDATA\Programs\Wktbox\bin"
[Environment]::SetEnvironmentVariable(
  "Path",
  "$dir;" + [Environment]::GetEnvironmentVariable("Path", "User"),
  "User"
)
```

Open a new terminal and verify:

```text
wktbox --version
```

## Manual checksum verification

Download the archive and `checksums.txt` from the same release. On Unix, the
published file names in `checksums.txt` include `dist/`, so normalize that
prefix when files are in the current directory:

```bash
archive=wktbox_0.1.0_linux_amd64.tar.gz
grep "dist/$archive" checksums.txt |
  sed 's#  dist/#  #' |
  sha256sum --check -
```

On macOS, replace `sha256sum --check -` with `shasum -a 256 -c -`.

On Windows:

```powershell
$archive = "wktbox_0.1.0_windows_amd64.zip"
$expected = (
  Select-String "dist/$archive" .\checksums.txt
).Line.Split(" ", [System.StringSplitOptions]::RemoveEmptyEntries)[0]
$actual = (Get-FileHash ".\$archive" -Algorithm SHA256).Hash
if ($actual -ne $expected) { throw "checksum mismatch" }
```

For provenance verification, continue with
[Release verification](release-verification.md).

## Update

Update by running the installer again. It downloads and verifies the latest
release before replacing the existing binary:

```bash
sh install.sh
```

```powershell
& .\install.ps1
```

To move to a specific release, repeat the fixed-version commands above. Agent
skills are versioned with the binary and update separately:

```bash
wktbox agents install --target all
```

If a managed skill has local edits, review the conflict before using `--force`;
see [Coding-agent integration](agents.md).

## Uninstall

Remove only the installed executable.

Linux or macOS:

```bash
rm "$HOME/.local/bin/wktbox"
```

Windows PowerShell:

```powershell
Remove-Item "$env:LOCALAPPDATA\Programs\Wktbox\bin\wktbox.exe"
```

Agent files are removed explicitly with `wktbox agents uninstall`. Existing
boxes and state are intentionally not deleted when the executable is removed.
Inspect them first with `wktbox list`; reinstall the same or a newer version to
perform lifecycle cleanup.
