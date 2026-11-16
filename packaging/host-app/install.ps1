# Install a trusted local PBS-X Windows preview bundle. Hashes are not signatures.
[CmdletBinding()]
param([switch]$Check, [switch]$Headless)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Test-Bundle([string]$Bundle) {
    $manifestPath = Join-Path $Bundle 'manifest.json'
    if ((Get-Item -LiteralPath $manifestPath).Length -gt 65536) { throw 'Manifest is too large' }
    $manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
    if ($manifest.schema -ne 1 -or $manifest.os -ne 'windows' -or $manifest.arch -ne 'amd64') { throw 'Expected a Windows amd64 bundle' }
    if ($manifest.version -notmatch '^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$') { throw 'Invalid bundle version' }
    $allowed = @('pbsx-agent.exe', 'pbsx-agentctl.exe', 'pbsx-desktop.exe', 'install.ps1')
    $names = @($manifest.files.PSObject.Properties.Name)
    foreach ($name in @('pbsx-agent.exe', 'pbsx-agentctl.exe', 'install.ps1')) {
        if ($name -notin $names) { throw "Missing manifest entry: $name" }
    }
    foreach ($entry in $manifest.files.PSObject.Properties) {
        if ($entry.Name -notin $allowed) { throw 'Unexpected bundle file list' }
        $file = Get-Item -LiteralPath (Join-Path $Bundle $entry.Name)
        if ($file.PSIsContainer -or ($file.Attributes -band [IO.FileAttributes]::ReparsePoint) -or $file.Length -ne $entry.Value.size) { throw "Unsafe or truncated file: $($entry.Name)" }
        if ((Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash -ne $entry.Value.sha256) { throw "Checksum mismatch: $($entry.Name)" }
        if ($entry.Name.EndsWith('.exe')) {
            $stream = [IO.File]::OpenRead($file.FullName)
            try {
                $reader = [IO.BinaryReader]::new($stream)
                if ($reader.ReadUInt16() -ne 0x5a4d) { throw 'Not a Windows executable' }
                $stream.Position = 0x3c
                $offset = $reader.ReadUInt32()
                if ($offset -gt $file.Length - 6) { throw 'Invalid Windows executable header' }
                $stream.Position = $offset
                if ($reader.ReadUInt32() -ne 0x4550 -or $reader.ReadUInt16() -ne 0x8664) { throw 'Not an amd64 Windows executable' }
            } finally { $stream.Dispose() }
        }
    }
    return $manifest
}

function Copy-Atomic([string]$Source, [string]$Destination) {
    $temporary = Join-Path (Split-Path $Destination) ('.pbsx-' + [Guid]::NewGuid().ToString('N'))
    try {
        Copy-Item -LiteralPath $Source -Destination $temporary
        if (Test-Path -LiteralPath $Destination) { [IO.File]::Replace($temporary, $Destination, $null) }
        else { [IO.File]::Move($temporary, $Destination) }
    } finally {
        if (Test-Path -LiteralPath $temporary) { Remove-Item -LiteralPath $temporary -Force }
    }
}

function Invoke-Agent([string]$Action) {
    & (Join-Path $target 'pbsx-agent.exe') $Action
    if ($LASTEXITCODE -ne 0) { throw "Agent $Action failed (exit $LASTEXITCODE)" }
}

if ($Check) {
    $manifest = Test-Bundle $PSScriptRoot
    Write-Output "Verified Windows bundle $($manifest.version)"
    exit 0
}
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { throw 'Run the installer from an Administrator PowerShell window' }
if (-not [Environment]::Is64BitOperatingSystem -or -not [Environment]::Is64BitProcess) { throw 'Use 64-bit PowerShell on amd64 Windows' }
$target = Join-Path ([Environment]::GetFolderPath('ProgramFiles')) 'PBS-X\Agent'
$base = Split-Path $target
foreach ($path in @($base, $target)) {
    if (Test-Path -LiteralPath $path) {
        $item = Get-Item -LiteralPath $path
        if (-not $item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw "Unsafe installation directory: $path" }
    } else { New-Item -ItemType Directory -Path $path | Out-Null }
}
# Protected staging and installation directories: users can execute, only
# administrators and SYSTEM can modify the service executable.
& icacls.exe $base /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' '*S-1-5-32-545:(OI)(CI)RX' | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Cannot protect application directory' }
& icacls.exe $target /reset | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Cannot protect agent directory' }
$staging = Join-Path $base ('.install-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $staging | Out-Null
try {
    foreach ($name in @('manifest.json', 'install.ps1', 'pbsx-agent.exe', 'pbsx-agentctl.exe', 'pbsx-desktop.exe')) {
        $source = Join-Path $PSScriptRoot $name
        if (Test-Path -LiteralPath $source) { Copy-Item -LiteralPath $source -Destination $staging }
    }
    $manifest = Test-Bundle $staging
    if (-not $Headless -and 'pbsx-desktop.exe' -notin @($manifest.files.PSObject.Properties.Name)) { throw 'Headless bundle: use -Headless' }
    $names = @('pbsx-agent.exe', 'pbsx-agentctl.exe')
    if (-not $Headless) { $names += 'pbsx-desktop.exe' }
    foreach ($name in $names) {
        $file = Join-Path $target $name
        if ((Test-Path -LiteralPath $file) -and ((Get-Item -LiteralPath $file).Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw "Unsafe existing application file: $file" }
    }
    $desktopPath = Join-Path $target 'pbsx-desktop.exe'
    if (Get-Process -Name 'pbsx-desktop' -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $desktopPath }) { throw 'Close the PBS-X desktop window before updating its files' }
    $service = Get-Service -Name PBSXAgent -ErrorAction SilentlyContinue
    if ($service) {
        $definition = Get-CimInstance Win32_Service -Filter "Name='PBSXAgent'"
        $expected = '"' + (Join-Path $target 'pbsx-agent.exe') + '"'
        if ($definition.PathName -ne $expected -and $definition.PathName -ne (Join-Path $target 'pbsx-agent.exe')) { throw 'Existing service uses another installation path; keep its current installer' }
    }
    $status = Get-ItemProperty -LiteralPath 'HKLM:\SOFTWARE\PBS-X\PublicStatus' -Name Snapshot -ErrorAction SilentlyContinue
    if ($status) {
        $value = $status.Snapshot | ConvertFrom-Json
        $age = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds() - $value.updated_at
        if ($value.PSObject.Properties['operation'] -and $age -ge -5 -and $age -le 15) { throw 'Backup/restore is active; install when idle' }
    }
    $running = $service -and $service.Status -eq 'Running'
    $recovery = Join-Path $base ('install-backups\' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $recovery -Force | Out-Null
    & icacls.exe $recovery /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Cannot protect recovery directory' }
    $previous = @()
    foreach ($name in $names) {
        $file = Join-Path $target $name
        if (Test-Path -LiteralPath $file) { Copy-Item -LiteralPath $file -Destination $recovery; $previous += $name }
    }
    $replaced = @()
    try {
        if ($service) { Stop-Service -Name PBSXAgent; $service.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(40)) }
        foreach ($name in $names) {
            Copy-Atomic (Join-Path $staging $name) (Join-Path $target $name)
            $replaced += $name
        }
        if (-not $service) { Invoke-Agent 'install' }
        if ($running) { Invoke-Agent 'start' }
    } catch {
        $failure = $_
        Stop-Service -Name PBSXAgent -ErrorAction SilentlyContinue
        if (-not $service -and 'pbsx-agent.exe' -in $replaced) {
            & (Join-Path $target 'pbsx-agent.exe') uninstall
        }
        foreach ($name in $replaced) {
            $file = Join-Path $target $name
            if ($name -in $previous) { Copy-Atomic (Join-Path $recovery $name) $file }
            else { Remove-Item -LiteralPath $file -Force }
        }
        if ($running) { Start-Service -Name PBSXAgent }
        Write-Warning "Previous executables retained in $recovery"
        throw $failure
    }
    if (-not $Headless) {
        $menu = Join-Path ([Environment]::GetFolderPath('CommonStartMenu')) 'Programs\PBS-X Agent.lnk'
        $shell = New-Object -ComObject WScript.Shell
        $shortcut = $shell.CreateShortcut($menu)
        $shortcut.TargetPath = $desktopPath
        $shortcut.Save()
    }
    Write-Output "Installed PBS-X $($manifest.version). Open PBS-X Agent to enroll. Existing credentials were preserved."
    if ($previous.Count) { Write-Output "Previous executables retained in $recovery" }
    else { Remove-Item -LiteralPath $recovery -Force }
} finally {
    Remove-Item -LiteralPath $staging -Recurse -Force
}
