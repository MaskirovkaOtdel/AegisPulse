# AegisPulse Smart Semantic Versioning & Release Filter Simulator
param(
    [string]$BaseTag = ""
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($BaseTag)) {
    try {
        $BaseTag = (git describe --tags --abbrev=0 --match "*v*" 2>$null).Trim()
    } catch {
        $BaseTag = ""
    }
}

if ([string]::IsNullOrWhiteSpace($BaseTag)) {
    $Prefix = "community-edition-v"
    $Major = 1; $Minor = 0; $Patch = 0
    $Range = "HEAD"
} else {
    if ($BaseTag -match "^(.*v)(\d+)\.(\d+)\.(\d+)$") {
        $Prefix = $Matches[1]
        $Major = [int]$Matches[2]
        $Minor = [int]$Matches[3]
        $Patch = [int]$Matches[4]
    } else {
        $Prefix = "v"; $Major = 1; $Minor = 0; $Patch = 0
    }
    $Range = "$BaseTag..HEAD"
}

$commits = git log $Range --oneline
$Bump = "none"
$Features = @()
$Fixes = @()
$Maintenance = @()

foreach ($line in $commits) {
    if ([string]::IsNullOrWhiteSpace($line)) { continue }
    $parts = $line -split " ", 2
    $sha = $parts[0]
    $msg = $parts[1]

    if ($msg -match "(\[skip release\]|\[no-release\])") {
        continue
    }

    if ($msg -match "(BREAKING CHANGE:|^[a-zA-Z]+(\([^)]+\))?!:|\[major\])") {
        $Bump = "major"
        $Features += "- **BREAKING**: $msg ($sha)"
    } elseif ($msg -match "^(feat|update)(\([^)]+\))?:|\[minor\]") {
        if ($Bump -ne "major") { $Bump = "minor" }
        $Features += "- $msg ($sha)"
    } elseif ($msg -match "^(fix|hotfix|perf|security)(\([^)]+\))?:|\[patch\]") {
        if ($Bump -eq "none") { $Bump = "patch" }
        $Fixes += "- $msg ($sha)"
    } else {
        $Maintenance += "- $msg ($sha)"
    }
}

Write-Host "=================================================================="
Write-Host "  AEGISPULSE SMART SEMANTIC VERSIONING & RELEASE FILTER REPORT"
Write-Host "=================================================================="
Write-Host "Previous Tag       : $(if ($BaseTag) { $BaseTag } else { 'None' })"
Write-Host "Commits Evaluated  : $($commits.Count)"
Write-Host "Detected Bump Type : $($Bump.ToUpper())"

if ($Bump -eq "none") {
    Write-Host "Release Decision   : SKIP (Only non-release commits detected; preventing tag spam)"
    exit 0
}

switch ($Bump) {
    "major" { $Major++; $Minor = 0; $Patch = 0 }
    "minor" { $Minor++; $Patch = 0 }
    "patch" { $Patch++ }
}

$NewTag = "$Prefix$Major.$Minor.$Patch"
Write-Host "Next Version Tag   : $NewTag"
Write-Host "Release Decision   : PUBLISH 1:1 TAG & GITHUB RELEASE"
Write-Host "------------------------------------------------------------------"
if ($Features.Count -gt 0) {
    Write-Host "Features & Updates :"
    $Features | ForEach-Object { Write-Host "  $_" }
}
if ($Fixes.Count -gt 0) {
    Write-Host "Patches & Fixes    :"
    $Fixes | ForEach-Object { Write-Host "  $_" }
}
if ($Maintenance.Count -gt 0) {
    Write-Host "Filtered Maint/Docs:"
    $Maintenance | ForEach-Object { Write-Host "  $_" }
}
Write-Host "=================================================================="
