BeforeAll {
    $ProjectRoot = (Resolve-Path (Join-Path $PSScriptRoot "../..")).Path
    . (Join-Path $ProjectRoot "scripts/install.ps1")
}

Describe "Wktbox Windows installer" {
    BeforeEach {
        $TestRoot = Join-Path $TestDrive ([Guid]::NewGuid().ToString())
        $ReleaseRoot = Join-Path $TestRoot "releases"
        $ReleaseDirectory = Join-Path $ReleaseRoot "download/v0.1.0"
        $Staging = Join-Path $TestRoot "staging"
        New-Item -ItemType Directory -Force -Path $ReleaseDirectory, $Staging |
            Out-Null

        Set-Content -NoNewline -Path (Join-Path $Staging "wktbox.exe") `
            -Value "wktbox 0.1.0 windows/amd64"
        Set-Content -Path (Join-Path $Staging "LICENSE") -Value "license"
        Set-Content -Path (Join-Path $Staging "README.md") -Value "readme"
        $Archive = Join-Path $ReleaseDirectory `
            "wktbox_0.1.0_windows_amd64.zip"
        Compress-Archive -Path (
            (Join-Path $Staging "wktbox.exe"),
            (Join-Path $Staging "LICENSE"),
            (Join-Path $Staging "README.md")
        ) -DestinationPath $Archive
        $Digest = (Microsoft.PowerShell.Utility\Get-FileHash `
                -LiteralPath $Archive -Algorithm SHA256).Hash.ToLowerInvariant()
        Set-Content -Path (Join-Path $ReleaseDirectory "checksums.txt") `
            -Value "$Digest  dist/$([IO.Path]::GetFileName($Archive))"
        Set-Content -Path (Join-Path $ReleaseRoot "latest") `
            -Value '{"tag_name":"v0.1.0"}'

        $InstallDirectory = Join-Path $TestRoot "install"
        $BaseUrl = [Uri]::new(
            (Resolve-Path $ReleaseRoot).Path,
            [UriKind]::Absolute
        ).AbsoluteUri.TrimEnd("/")

        Mock Get-WktboxOperatingSystem { "windows" }
        Mock Get-WktboxArchitecture { "amd64" }
        Mock Get-FileHash -ParameterFilter { $Algorithm -eq "SHA256" } {
            $stream = [IO.File]::OpenRead($LiteralPath)
            try {
                $bytes = [Security.Cryptography.SHA256]::HashData($stream)
                [PSCustomObject]@{
                    Hash = [Convert]::ToHexString($bytes)
                }
            }
            finally {
                $stream.Dispose()
            }
        }
    }

    It "installs a verified archive and supports an idempotent update" {
        Invoke-WktboxInstaller `
            -Version "v0.1.0" `
            -InstallDir $InstallDirectory `
            -BaseUrl $BaseUrl

        $Target = Join-Path $InstallDirectory "wktbox.exe"
        Get-Content -Raw $Target |
            Should -Be "wktbox 0.1.0 windows/amd64"
        Should -Invoke Get-FileHash -Times 1 -Exactly `
            -ParameterFilter { $Algorithm -eq "SHA256" }

        Set-Content -NoNewline -Path $Target -Value "old version"
        Invoke-WktboxInstaller `
            -Version "0.1.0" `
            -InstallDir $InstallDirectory `
            -BaseUrl $BaseUrl
        Get-Content -Raw $Target |
            Should -Be "wktbox 0.1.0 windows/amd64"
    }

    It "resolves latest only when no version is supplied" {
        Invoke-WktboxInstaller `
            -InstallDir $InstallDirectory `
            -BaseUrl $BaseUrl

        Test-Path (Join-Path $InstallDirectory "wktbox.exe") |
            Should -BeTrue
    }

    It "preserves an existing executable when checksum verification fails" {
        New-Item -ItemType Directory -Force -Path $InstallDirectory |
            Out-Null
        $Target = Join-Path $InstallDirectory "wktbox.exe"
        Set-Content -NoNewline -Path $Target -Value "old version"
        Set-Content `
            -Path (Join-Path $ReleaseDirectory "checksums.txt") `
            -Value "$("0" * 64)  dist/$([IO.Path]::GetFileName($Archive))"

        {
            Invoke-WktboxInstaller `
                -Version "0.1.0" `
                -InstallDir $InstallDirectory `
                -BaseUrl $BaseUrl
        } | Should -Throw "*checksum*"
        Get-Content -Raw $Target | Should -Be "old version"
    }

    It "preserves an existing executable when extraction fails" {
        New-Item -ItemType Directory -Force -Path $InstallDirectory |
            Out-Null
        $Target = Join-Path $InstallDirectory "wktbox.exe"
        Set-Content -NoNewline -Path $Target -Value "old version"
        Set-Content -NoNewline -Path $Archive -Value "not a zip"
        $Digest = (Microsoft.PowerShell.Utility\Get-FileHash `
                -LiteralPath $Archive -Algorithm SHA256).Hash.ToLowerInvariant()
        Set-Content `
            -Path (Join-Path $ReleaseDirectory "checksums.txt") `
            -Value "$Digest  dist/$([IO.Path]::GetFileName($Archive))"

        {
            Invoke-WktboxInstaller `
                -Version "0.1.0" `
                -InstallDir $InstallDirectory `
                -BaseUrl $BaseUrl
        } | Should -Throw
        Get-Content -Raw $Target | Should -Be "old version"
    }

    It "prints precise PATH guidance without mutating PATH" {
        $OriginalPath = $env:PATH
        $output = Invoke-WktboxInstaller `
            -Version "0.1.0" `
            -InstallDir $InstallDirectory `
            -BaseUrl $BaseUrl |
            Out-String

        $env:PATH | Should -BeExactly $OriginalPath
        $output | Should -Match (
            [Regex]::Escape("[Environment]::SetEnvironmentVariable")
        )
        $output | Should -Match ([Regex]::Escape($InstallDirectory))
    }
}
