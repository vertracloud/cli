# Instala a CLI da Vertra Cloud no Windows.
#   irm https://cli.vertracloud.app/install | iex
# $env:VERTRA_VERSION = "vX.Y.Z" fixa a versão; $env:VERTRA_INSTALL_DIR troca a pasta (padrão %USERPROFILE%\.vertracloud\bin, junto da configuração).
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$repo = "https://github.com/vertracloud/cli"
$dir = if ($env:VERTRA_INSTALL_DIR) { $env:VERTRA_INSTALL_DIR } else { Join-Path $HOME ".vertracloud\bin" }

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { "amd64" }
    "ARM64" { "arm64" }
    default { throw "vertra: unsupported architecture $env:PROCESSOR_ARCHITECTURE" }
}

$version = $env:VERTRA_VERSION
if (-not $version) {
    $version = (Invoke-RestMethod "https://api.github.com/repos/vertracloud/cli/releases/latest").tag_name
}
if ($version -notmatch '^v\d') { throw "vertra: could not resolve the latest version" }

$asset = "vertra_${version}_windows_${arch}.zip"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    Write-Host "Downloading vertra $version (windows/$arch)..."
    Invoke-WebRequest "$repo/releases/download/$version/$asset" -OutFile (Join-Path $tmp $asset) -UseBasicParsing
    Invoke-WebRequest "$repo/releases/download/$version/checksums.txt" -OutFile (Join-Path $tmp "checksums.txt") -UseBasicParsing

    $expected = (Get-Content (Join-Path $tmp "checksums.txt") | Where-Object { ($_ -split '\s+')[1] -eq $asset } | ForEach-Object { ($_ -split '\s+')[0] })
    $actual = (Get-FileHash (Join-Path $tmp $asset) -Algorithm SHA256).Hash
    if (-not $expected -or $expected -ne $actual) { throw "vertra: checksum mismatch for $asset" }

    Expand-Archive (Join-Path $tmp $asset) -DestinationPath $tmp -Force
    New-Item -ItemType Directory -Path $dir -Force | Out-Null
    Move-Item (Join-Path $tmp "vertra.exe") (Join-Path $dir "vertra.exe") -Force
} finally {
    Remove-Item $tmp -Recurse -Force
}
Write-Host "Installed $dir\vertra.exe"

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (($userPath -split ';') -notcontains $dir) {
    $newPath = if ($userPath) { "$($userPath.TrimEnd(';'));$dir" } else { $dir }
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    $env:Path = "$env:Path;$dir"
    Write-Host "Added $dir to your PATH. Open a new terminal if 'vertra' is not found."
}
Write-Host "Run 'vertra --help' to get started."
