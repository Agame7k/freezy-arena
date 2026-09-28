# Copyright 2026 Team 254. All Rights Reserved.
#
# Starts a multi-field test cluster on one machine: a hub and two field nodes, each with its own port and database,
# with field hardware simulated so that no robots, access point or PLC are needed.
#
#   .\scripts\dev_cluster.ps1                 # start (or restart) hub :8080, Field 1 :8081, Field 2 :8082
#   .\scripts\dev_cluster.ps1 -Fresh          # delete the cluster databases first
#   .\scripts\dev_cluster.ps1 -Stop           # stop the cluster
#
# Logs go to dev_cluster\*.log. Run .\scripts\simulate_event.ps1 afterwards for a hands-off dry run.

param(
  [switch]$Fresh,
  [switch]$Stop,
  [int]$HubPort = 8080,
  [string]$Secret = "dev-secret"
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$clusterDir = Join-Path $root "dev_cluster"
$pidFile = Join-Path $clusterDir "pids.txt"

function Stop-Cluster {
  if (Test-Path $pidFile) {
    foreach ($processId in Get-Content $pidFile) {
      try {
        Stop-Process -Id ([int]$processId) -Force -ErrorAction Stop
        Write-Host "Stopped process $processId"
      } catch {
      }
    }
    Remove-Item $pidFile -Force
  }
}

Stop-Cluster
if ($Stop) {
  exit 0
}

New-Item -ItemType Directory -Force $clusterDir | Out-Null
if ($Fresh) {
  Get-ChildItem $clusterDir -Filter "*.db" | Remove-Item -Force
}

Push-Location $root
try {
  Write-Host "Building cheesy-arena..."
  go build -o (Join-Path $clusterDir "cheesy-arena.exe") .
  if ($LASTEXITCODE -ne 0) {
    throw "Build failed"
  }

  $binary = Join-Path $clusterDir "cheesy-arena.exe"
  $hubAddress = "http://localhost:$HubPort"
  $instances = @(
    @{ Name = "hub"; Args = @("-port", $HubPort, "-db", "dev_cluster/hub.db", "-role", "hub", "-secret", $Secret,
        "-simulate", "-auto-approve-nodes") },
    @{ Name = "field1"; Args = @("-port", ($HubPort + 1), "-db", "dev_cluster/field1.db", "-role", "node",
        "-field-id", "1", "-hub", $hubAddress, "-secret", $Secret, "-simulate") },
    @{ Name = "field2"; Args = @("-port", ($HubPort + 2), "-db", "dev_cluster/field2.db", "-role", "node",
        "-field-id", "2", "-hub", $hubAddress, "-secret", $Secret, "-simulate") }
  )

  $pids = @()
  foreach ($instance in $instances) {
    $log = Join-Path $clusterDir "$($instance.Name).log"
    $process = Start-Process -FilePath $binary -ArgumentList $instance.Args -WorkingDirectory $root `
      -RedirectStandardError $log -RedirectStandardOutput "$log.out" -PassThru -WindowStyle Hidden
    $pids += $process.Id
    Write-Host ("Started {0,-7} pid {1,-6} {2}" -f $instance.Name, $process.Id, ($instance.Args -join " "))
    Start-Sleep -Milliseconds 500
  }
  $pids | Set-Content $pidFile
} finally {
  Pop-Location
}

Write-Host ""
Write-Host "Hub:     http://localhost:$HubPort/event_control"
Write-Host "Field 1: http://localhost:$($HubPort + 1)/match_play"
Write-Host "Field 2: http://localhost:$($HubPort + 2)/match_play"
Write-Host "Stop with: .\scripts\dev_cluster.ps1 -Stop"
