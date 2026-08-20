param(
    [string] $Version = $env:SANTAIZI_AGENT_VERSION
)

$ErrorActionPreference = "Stop"
$AgentRepository = if ($env:SANTAIZI_AGENT_REPO) { $env:SANTAIZI_AGENT_REPO } else { "santaizi-group/santaizi-agent" }
$InstallDirectory = if ($env:SANTAIZI_AGENT_PATH) { $env:SANTAIZI_AGENT_PATH } else { "C:\santaizi" }
$AgentBinary = Join-Path $InstallDirectory "santaizi-agent.exe"
$ConfigurationPath = if ($env:SANTAIZI_AGENT_CONFIG) { $env:SANTAIZI_AGENT_CONFIG } else { "C:\ProgramData\santaizi\agent.yaml" }

if ($PSVersionTable.PSVersion.Major -lt 5) {
    throw "Santaizi Agent requires PowerShell 5 or later."
}
if (-not (Test-Path -LiteralPath $AgentBinary)) {
    throw "Santaizi agent is not installed at $AgentBinary. Run the install script first, not this upgrade script."
}
if (-not (Test-Path -LiteralPath $ConfigurationPath)) {
    throw "Missing config file: $ConfigurationPath. Upgrade does not write secrets; finish installation first."
}

function Get-NormalizedVersion([string] $Value) {
    if ([string]::IsNullOrWhiteSpace($Value)) { return "" }
    $Trimmed = $Value.Trim()
    if ($Trimmed.StartsWith("v") -or $Trimmed.StartsWith("V")) {
        return $Trimmed.Substring(1)
    }
    return $Trimmed
}

function Get-CurrentVersion {
    try {
        $Output = & $AgentBinary --config $ConfigurationPath --version 2>$null
        if ($LASTEXITCODE -eq 0 -and $Output) {
            return ([string]$Output).Trim()
        }
    } catch { }
    return ""
}

$Architecture = if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } elseif ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }
$Requested = Get-NormalizedVersion $Version
if ($Requested) {
    $TargetTag = "v$Requested"
} else {
    $Release = Invoke-RestMethod -Uri "https://api.github.com/repos/$AgentRepository/releases/latest" -UseBasicParsing
    if (-not $Release.tag_name) { throw "Unable to determine the latest Santaizi Agent release." }
    $TargetTag = [string]$Release.tag_name
}

$Installed = Get-CurrentVersion
if ($Installed) { Write-Host "Current version: $Installed" }
Write-Host "Target version: $TargetTag"

if (-not $Requested -and (Get-NormalizedVersion $Installed) -eq (Get-NormalizedVersion $TargetTag)) {
    Write-Host "Already at the target version."
    return
}

try { & $AgentBinary service stop | Out-Null } catch { }
Stop-Service -Name "santaizi-agent" -Force -ErrorAction SilentlyContinue

$ArchiveName = "santaizi-agent_windows_$Architecture.zip"
$DownloadUrl = "https://github.com/$AgentRepository/releases/download/$TargetTag/$ArchiveName"
$TemporaryRoot = Join-Path $env:TEMP ("santaizi-agent-upgrade-" + [Guid]::NewGuid().ToString("N"))
$ArchivePath = Join-Path $TemporaryRoot $ArchiveName
$ExtractPath = Join-Path $TemporaryRoot "extract"

New-Item -ItemType Directory -Path $TemporaryRoot, $ExtractPath -Force | Out-Null
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    Write-Host "Downloading $DownloadUrl ..."
    Invoke-WebRequest -Uri $DownloadUrl -OutFile $ArchivePath -UseBasicParsing
    Expand-Archive -LiteralPath $ArchivePath -DestinationPath $ExtractPath -Force
    $Extracted = Join-Path $ExtractPath "santaizi-agent.exe"
    if (-not (Test-Path -LiteralPath $Extracted)) {
        throw "Archive does not contain santaizi-agent.exe."
    }
    New-Item -ItemType Directory -Path $InstallDirectory -Force | Out-Null
    Copy-Item -LiteralPath $Extracted -Destination $AgentBinary -Force
} finally {
    if (Test-Path $TemporaryRoot) { Remove-Item -LiteralPath $TemporaryRoot -Recurse -Force }
}

try {
    & $AgentBinary service restart | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "restart failed" }
} catch {
    try {
        & $AgentBinary service start | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "start failed" }
    } catch {
        throw "Binary replaced, but the service failed to start. Run: $AgentBinary service start"
    }
}

$Installed = Get-CurrentVersion
if ($Installed) {
    Write-Host "Agent upgraded to $Installed."
} else {
    Write-Host "Agent upgraded to $TargetTag."
}
