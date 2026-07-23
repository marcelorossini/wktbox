param(
    [string]$Version,
    [string]$InstallDir,
    [string]$BaseUrl = (
        "https://github.com/marcelorossini/wktbox/releases"
    )
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

function Get-WktboxOperatingSystem {
    if ([Runtime.InteropServices.RuntimeInformation]::IsOSPlatform(
            [Runtime.InteropServices.OSPlatform]::Windows
        )) {
        return "windows"
    }
    throw "unsupported operating system"
}

function Get-WktboxArchitecture {
    $architecture = (
        [Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
    ).ToLowerInvariant()
    switch ($architecture) {
        { $_ -in @("x64", "amd64") } { return "amd64" }
        { $_ -in @("arm64", "aarch64") } { return "arm64" }
        default { throw "unsupported architecture: $architecture" }
    }
}

function ConvertTo-WktboxVersion {
    param([Parameter(Mandatory)][string]$Value)

    $normalized = $Value -replace "^v", ""
    if ($normalized -notmatch (
            "^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$"
        )) {
        throw "invalid version '$Value'; expected X.Y.Z or vX.Y.Z"
    }
    return $normalized
}

function Copy-WktboxReleaseFile {
    param(
        [Parameter(Mandatory)][string]$Uri,
        [Parameter(Mandatory)][string]$Destination
    )

    $parsed = [Uri]$Uri
    if ($parsed.IsFile) {
        Copy-Item -LiteralPath $parsed.LocalPath -Destination $Destination
        return
    }
    Invoke-WebRequest `
        -Uri $Uri `
        -OutFile $Destination `
        -MaximumRetryCount 3 `
        -UseBasicParsing
}

function Invoke-WktboxInstaller {
    [CmdletBinding()]
    param(
        [string]$Version,
        [string]$InstallDir,
        [string]$BaseUrl = (
            "https://github.com/marcelorossini/wktbox/releases"
        )
    )

    $operatingSystem = Get-WktboxOperatingSystem
    if ($operatingSystem -ne "windows") {
        throw "unsupported operating system: $operatingSystem"
    }
    $architecture = Get-WktboxArchitecture
    if ([string]::IsNullOrWhiteSpace($InstallDir)) {
        if ([string]::IsNullOrWhiteSpace($env:LOCALAPPDATA)) {
            throw "LOCALAPPDATA is required when InstallDir is not supplied"
        }
        $InstallDir = Join-Path $env:LOCALAPPDATA "Programs\Wktbox\bin"
    }
    $base = $BaseUrl.TrimEnd("/")
    $temporary = Join-Path ([IO.Path]::GetTempPath()) (
        "wktbox-" + [Guid]::NewGuid().ToString("N")
    )
    $candidate = $null
    New-Item -ItemType Directory -Path $temporary | Out-Null

    try {
        if ([string]::IsNullOrWhiteSpace($Version)) {
            $apiUrl = (
                "https://api.github.com/repos/" +
                "marcelorossini/wktbox/releases/latest"
            )
            if ($base -ne (
                    "https://github.com/marcelorossini/wktbox/releases"
                )) {
                $apiUrl = "$base/latest"
            }
            $latestPath = Join-Path $temporary "latest.json"
            Copy-WktboxReleaseFile `
                -Uri $apiUrl `
                -Destination $latestPath
            $latest = Get-Content -Raw -LiteralPath $latestPath |
                ConvertFrom-Json
            if ([string]::IsNullOrWhiteSpace($latest.tag_name)) {
                throw "latest release response does not contain tag_name"
            }
            $Version = [string]$latest.tag_name
        }
        $normalizedVersion = ConvertTo-WktboxVersion -Value $Version

        $archiveName = (
            "wktbox_{0}_{1}_{2}.zip" -f
            $normalizedVersion,
            $operatingSystem,
            $architecture
        )
        $releaseUrl = "$base/download/v$normalizedVersion"
        $archivePath = Join-Path $temporary $archiveName
        $checksumsPath = Join-Path $temporary "checksums.txt"
        Copy-WktboxReleaseFile `
            -Uri "$releaseUrl/$archiveName" `
            -Destination $archivePath
        Copy-WktboxReleaseFile `
            -Uri "$releaseUrl/checksums.txt" `
            -Destination $checksumsPath

        $expectedDigest = $null
        foreach ($line in Get-Content -LiteralPath $checksumsPath) {
            $parts = $line.Trim() -split "\s+", 2
            if (
                $parts.Count -eq 2 -and
                [IO.Path]::GetFileName($parts[1]) -eq $archiveName
            ) {
                $expectedDigest = $parts[0]
                break
            }
        }
        if ([string]::IsNullOrWhiteSpace($expectedDigest)) {
            throw "checksum for $archiveName is missing from checksums.txt"
        }
        $actualDigest = (
            Get-FileHash -LiteralPath $archivePath -Algorithm SHA256
        ).Hash
        if (
            $actualDigest.ToLowerInvariant() -ne
            $expectedDigest.ToLowerInvariant()
        ) {
            throw "checksum verification failed for $archiveName"
        }

        $extractDirectory = Join-Path $temporary "extract"
        Expand-Archive `
            -LiteralPath $archivePath `
            -DestinationPath $extractDirectory
        $executable = Join-Path $extractDirectory "wktbox.exe"
        if (-not (Test-Path -LiteralPath $executable -PathType Leaf)) {
            throw "archive $archiveName does not contain wktbox.exe"
        }

        New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
        $target = Join-Path $InstallDir "wktbox.exe"
        $candidate = Join-Path $InstallDir (
            ".wktbox-" + [Guid]::NewGuid().ToString("N") + ".tmp"
        )
        Copy-Item -LiteralPath $executable -Destination $candidate
        [IO.File]::Move($candidate, $target, $true)
        $candidate = $null

        Write-Output "Installed Wktbox $normalizedVersion at $target"
        $pathEntries = $env:PATH -split [IO.Path]::PathSeparator
        if ($InstallDir -notin $pathEntries) {
            Write-Output "Add Wktbox to the user PATH with:"
            Write-Output (
                '[Environment]::SetEnvironmentVariable(' +
                '"Path", "' + $InstallDir + ';" + ' +
                '[Environment]::GetEnvironmentVariable(' +
                '"Path", "User"), "User")'
            )
        }
    }
    finally {
        if ($candidate -and (Test-Path -LiteralPath $candidate)) {
            Remove-Item -Force -LiteralPath $candidate
        }
        if (Test-Path -LiteralPath $temporary) {
            Remove-Item -Recurse -Force -LiteralPath $temporary
        }
    }
}

if ($MyInvocation.InvocationName -ne ".") {
    Invoke-WktboxInstaller @PSBoundParameters
}
