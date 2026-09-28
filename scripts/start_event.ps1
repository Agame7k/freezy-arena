# Copyright 2026 Team 254. All Rights Reserved.
#
# One command to start a whole multi-field event on this computer for testing: the hub and both field nodes, all in
# simulation mode (no robots or field hardware needed), connected and approved, with Event Control opened in the
# browser. Add -RunAll to also play the whole event automatically.
#
#   .\scripts\start_event.ps1                          # empty event; press "Simulate the whole event" on Event Control
#   .\scripts\start_event.ps1 -RunAll                  # ...and play it through to the champion right away
#   .\scripts\start_event.ps1 -Db nmrc_cmrc.db         # start from an existing event database (e.g. the NMRC/CMRC one)
#   .\scripts\start_event.ps1 -Db nmrc_cmrc.db -RunAll # simulate it (on a copy, so the real database is untouched)
#   .\scripts\start_event.ps1 -Stop                    # stop everything
#   .\scripts\start_event.ps1 -HubPort 9080            # use ports 9080-9082 if 8080-8082 are taken
#
# Or double-click scripts\start_event.cmd and scripts\stop_event.cmd, which work even where PowerShell scripts are
# blocked by the execution policy (the start_event.cmd also passes on any of the options above).
#
# Logs and databases go in dev_cluster\ (not checked in).

param(
  # Event database for the hub. Defaults to a scratch database in dev_cluster\.
  [string]$Db = "",
  # Start from empty databases.
  [switch]$Fresh,
  # Play the whole event automatically once everything is up.
  [switch]$RunAll,
  # Seconds between simulated matches on each field when using -RunAll.
  [int]$IntervalSec = 1,
  # With -RunAll and a -Db outside dev_cluster\, simulate on that database itself instead of a copy.
  [switch]$InPlace,
  [switch]$NoBrowser,
  [switch]$Stop,
  [int]$HubPort = 8080,
  [string]$Secret = "dev-secret"
)

$ErrorActionPreference = "Stop"
# Show what went wrong without PowerShell's stack details, and exit with a failure code.
trap {
  Write-Host $_.Exception.Message -ForegroundColor Red
  exit 1
}
$root = Split-Path -Parent $PSScriptRoot
$clusterDir = Join-Path $root "dev_cluster"
$pidFile = Join-Path $clusterDir "pids.txt"
$hubUrl = "http://localhost:$HubPort"

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

# Returns the process listening on the given local port, or $null if the port is free.
function Get-PortOwner([int]$port) {
  $listener = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($null -eq $listener) {
    return $null
  }
  return Get-Process -Id $listener.OwningProcess -ErrorAction SilentlyContinue
}

# Refuses to start if another program (e.g. the real event's Cheesy Arena) already uses one of the ports, since the
# fields would otherwise connect to that program instead of this test hub.
function Assert-PortsFree([int[]]$ports) {
  foreach ($port in $ports) {
    $owner = Get-PortOwner $port
    if ($null -ne $owner) {
      throw ("Port $port is already in use by $($owner.ProcessName) (process $($owner.Id)). Stop it, or pick other " +
        "ports with -HubPort (e.g. -HubPort 9080 uses 9080-9082).")
    }
  }
}

# Shows the end of an instance's log, for when it didn't start.
function Show-LogTail([string]$name) {
  $log = Join-Path $clusterDir "$name.log"
  if (Test-Path $log) {
    Write-Host "Last lines of ${log}:" -ForegroundColor Yellow
    Get-Content $log -Tail 8 | ForEach-Object { Write-Host "  $_" }
  }
}

# Waits until the instance's own process is serving its port, failing early if the process exits.
function Wait-Instance($instance, $process) {
  $deadline = (Get-Date).AddSeconds(30)
  while ($true) {
    if ($process.HasExited) {
      Show-LogTail $instance.Name
      throw "The $($instance.Name) instance stopped while starting; its log above says why."
    }
    $owner = Get-PortOwner $instance.Port
    if ($null -ne $owner -and $owner.Id -eq $process.Id) {
      try {
        Invoke-WebRequest -UseBasicParsing -Uri "$($instance.Url)/" -TimeoutSec 5 | Out-Null
        return
      } catch {
      }
    }
    if ((Get-Date) -gt $deadline) {
      Show-LogTail $instance.Name
      throw "The $($instance.Name) instance didn't start serving $($instance.Url) within 30 seconds."
    }
    Start-Sleep -Milliseconds 300
  }
}

Stop-Cluster
if ($Stop) {
  exit 0
}
Assert-PortsFree @($HubPort, ($HubPort + 1), ($HubPort + 2))
New-Item -ItemType Directory -Force $clusterDir | Out-Null

# Pick the hub database, working on a copy of a real event database when simulating it.
if ($Db -eq "") {
  $hubDb = Join-Path $clusterDir "hub.db"
} else {
  $hubDb = (Resolve-Path $Db).Path
  $insideCluster = $hubDb.StartsWith($clusterDir, [StringComparison]::OrdinalIgnoreCase)
  if ($RunAll -and -not $InPlace -and -not $insideCluster) {
    $copy = Join-Path $clusterDir ("{0}_sim.db" -f [IO.Path]::GetFileNameWithoutExtension($hubDb))
    Copy-Item $hubDb $copy -Force
    Write-Host "Simulating on a copy of $Db ($copy) so that the original is untouched."
    $hubDb = $copy
  }
}
# Each hub database gets its own field databases, so that results from another event never leak across.
$baseName = [IO.Path]::GetFileNameWithoutExtension($hubDb)
$fieldDbs = @((Join-Path $clusterDir "${baseName}_field1.db"), (Join-Path $clusterDir "${baseName}_field2.db"))
if ($Fresh) {
  foreach ($file in $fieldDbs) {
    Remove-Item $file -ErrorAction SilentlyContinue
  }
  if ($hubDb.StartsWith($clusterDir, [StringComparison]::OrdinalIgnoreCase) -and $Db -eq "") {
    Remove-Item $hubDb -ErrorAction SilentlyContinue
  }
}

Push-Location $root
try {
  Write-Host "Building cheesy-arena..."
  $binary = Join-Path $clusterDir "cheesy-arena.exe"
  go build -o $binary .
  if ($LASTEXITCODE -ne 0) {
    throw "Build failed"
  }

  $instances = @(
    @{ Name = "hub"; Port = $HubPort; Url = $hubUrl; Args = @("-port", $HubPort, "-db", "`"$hubDb`"", "-role", "hub",
        "-secret", $Secret, "-simulate", "-auto-approve-nodes") },
    @{ Name = "field1"; Port = ($HubPort + 1); Url = "http://localhost:$($HubPort + 1)"; Args = @("-port",
        ($HubPort + 1), "-db", "`"$($fieldDbs[0])`"", "-role", "node", "-field-id", "1", "-hub", $hubUrl, "-secret",
        $Secret, "-simulate") },
    @{ Name = "field2"; Port = ($HubPort + 2); Url = "http://localhost:$($HubPort + 2)"; Args = @("-port",
        ($HubPort + 2), "-db", "`"$($fieldDbs[1])`"", "-role", "node", "-field-id", "2", "-hub", $hubUrl, "-secret",
        $Secret, "-simulate") }
  )
  $pids = @()
  $processes = @{}
  foreach ($instance in $instances) {
    Write-Host "Starting $($instance.Name) on port $($instance.Port)..."
    $log = Join-Path $clusterDir "$($instance.Name).log"
    $process = Start-Process -FilePath $binary -ArgumentList $instance.Args -WorkingDirectory $root `
      -RedirectStandardError $log -RedirectStandardOutput "$log.out" -PassThru -WindowStyle Hidden
    $processes[$instance.Name] = $process
    $pids += $process.Id
    $pids | Set-Content $pidFile
    Start-Sleep -Milliseconds 300
  }
  try {
    foreach ($instance in $instances) {
      Wait-Instance $instance $processes[$instance.Name]
    }
  } catch {
    Stop-Cluster | Out-Null
    throw
  }
} finally {
  Pop-Location
}

# Wait for both fields to connect to the hub (they are approved automatically), and say which didn't.
$problems = @()
$deadline = (Get-Date).AddSeconds(20)
foreach ($fieldId in 1, 2) {
  $port = $HubPort + $fieldId
  $status = $null
  while ($true) {
    try {
      $status = (Invoke-RestMethod -Uri "http://localhost:$port/setup/node/status").Status
    } catch {
    }
    if (($status.Connected -and $status.Approved) -or (Get-Date) -gt $deadline) {
      break
    }
    Start-Sleep -Milliseconds 500
  }
  if ($null -eq $status) {
    $problems += "Couldn't get Field ${fieldId}'s status from http://localhost:$port (log: dev_cluster\field$fieldId.log)"
  } elseif (-not ($status.Connected -and $status.Approved)) {
    $problems += "Field ${fieldId} isn't connected to the hub: $($status.LastError) (log: dev_cluster\field$fieldId.log)"
  }
}

Write-Host ""
Write-Host "Hub:     $hubUrl/event_control   (database $hubDb)"
Write-Host "Field 1: http://localhost:$($HubPort + 1)/match_play"
Write-Host "Field 2: http://localhost:$($HubPort + 2)/match_play"
Write-Host "Shared secret: $Secret"
Write-Host "Stop with: .\scripts\start_event.ps1 -Stop   (or double-click scripts\stop_event.cmd)"
if ($problems.Count -gt 0) {
  Write-Host ""
  foreach ($problem in $problems) {
    Write-Host $problem -ForegroundColor Yellow
  }
} else {
  Write-Host "Both fields are connected to the hub." -ForegroundColor Green
}
if (-not $NoBrowser) {
  Start-Process "$hubUrl/event_control"
}

if ($RunAll) {
  Write-Host ""
  Write-Host "Simulating the whole event..."
  Invoke-WebRequest -UseBasicParsing -Method Post -Uri "$hubUrl/event_control/simulate_all" `
    -Body @{ intervalSec = $IntervalSec } | Out-Null
  $lastStatus = ""
  $deadline = (Get-Date).AddMinutes(30)
  while ((Get-Date) -lt $deadline) {
    Start-Sleep -Seconds 3
    $html = (Invoke-WebRequest -UseBasicParsing -Uri "$hubUrl/event_control/checklist").Content
    $running = $html -match "Stop simulating"
    $status = ""
    if ($html -match '<div class="mt-1 fw-semibold">([^<]*)</div>') {
      $status = [Net.WebUtility]::HtmlDecode($Matches[1])
    }
    if ($status -ne $lastStatus) {
      Write-Host "  $status"
      $lastStatus = $status
    }
    if (-not $running) {
      break
    }
  }
}
