# Installs the latest envgrid release for Windows.
#   irm https://raw.githubusercontent.com/Achal13jain/envgrid/main/install.ps1 | iex
$ErrorActionPreference = 'Stop'
$repo = 'Achal13jain/envgrid'
$name = 'envgrid_windows_amd64'
$base = "https://github.com/$repo/releases/latest/download"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    Write-Host "Downloading $name..."
    Invoke-WebRequest "$base/$name.zip" -OutFile "$tmp\$name.zip" -UseBasicParsing
    Invoke-WebRequest "$base/checksums.txt" -OutFile "$tmp\checksums.txt" -UseBasicParsing
    $line = Get-Content "$tmp\checksums.txt" | Where-Object { $_ -match " \./$name\.zip$" }
    $expected = if ($line) { ($line -split ' ')[0] } else { '' }
    $actual = (Get-FileHash "$tmp\$name.zip" -Algorithm SHA256).Hash.ToLower()
    if (-not $expected -or $expected -ne $actual) { throw 'checksum mismatch, not installing' }
    Expand-Archive "$tmp\$name.zip" -DestinationPath $tmp
    $dir = Join-Path $env:LOCALAPPDATA 'envgrid'
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    Copy-Item "$tmp\$name\envgrid.exe" "$dir\envgrid.exe" -Force
    $path = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (($path -split ';') -notcontains $dir) {
        [Environment]::SetEnvironmentVariable('Path', "$path;$dir", 'User')
        $env:Path = "$env:Path;$dir"
    }
    Write-Host "Installed $(& "$dir\envgrid.exe" version) to $dir\envgrid.exe"
    Write-Host "Next: run 'envgrid genkey', then see https://github.com/$repo#quick-start"
} finally {
    Remove-Item -Recurse -Force $tmp
}
