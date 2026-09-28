# Copyright 2026 Team 254. All Rights Reserved.
#
# Runs a simulated multi-conference, multi-field event against a cluster started with dev_cluster.ps1: seeds teams and
# a qualification schedule on the hub, has both field nodes play their matches with random scores, runs alliance
# selection automatically, then plays the playoffs through to the champion.
#
#   .\scripts\dev_cluster.ps1 -Fresh
#   .\scripts\simulate_event.ps1 -Teams 36 -IntervalSec 2

param(
  [int]$HubPort = 8080,
  [int]$Teams = 36,
  [int]$MatchesPerTeam = 8,
  [int]$IntervalSec = 2,
  [int]$TimeoutMin = 20,
  # Optional conference names, e.g. -Conf1Name NMRC -Conf1Short NM -Conf2Name CMRC -Conf2Short CM.
  [string]$Conf1Name = "",
  [string]$Conf1Short = "",
  [string]$Conf2Name = "",
  [string]$Conf2Short = "",
  # Optional roster file with "number,conference short name,nickname" lines, e.g. docs\nmrc_cmrc_teams.csv.
  [string]$Roster = "",
  # Qualification field assignment: alternate, blocks or dynamic (nodes claim matches from the hub one at a time).
  [ValidateSet("alternate", "blocks", "dynamic")]
  [string]$FieldAssignment = "alternate",
  # Minimum rest between a team's matches in dynamic mode; the simulation plays much faster than real matches.
  [int]$MinTurnaroundSec = 0
)

$ErrorActionPreference = "Stop"
$hub = "http://localhost:$HubPort"
$nodes = @("http://localhost:$($HubPort + 1)", "http://localhost:$($HubPort + 2)")

function Invoke-Dev([string]$url) {
  return Invoke-RestMethod -Method Post -Uri $url
}

function Wait-ForMatches([string]$type) {
  $deadline = (Get-Date).AddMinutes($TimeoutMin)
  while ((Get-Date) -lt $deadline) {
    $matches = Invoke-RestMethod -Uri "$hub/api/matches/$type"
    $remaining = @($matches | Where-Object { -not $_.Result }).Count
    $total = @($matches).Count
    Write-Host ("  {0}: {1}/{2} played" -f $type, ($total - $remaining), $total)
    if ($total -gt 0 -and $remaining -eq 0) {
      return
    }
    Start-Sleep -Seconds 5
  }
  throw "Timed out waiting for $type matches to finish"
}

Write-Host "Waiting for the cluster to come up..."
foreach ($url in @($hub) + $nodes) {
  $deadline = (Get-Date).AddSeconds(30)
  while ($true) {
    try {
      Invoke-RestMethod -Uri "$url/api/dev/status" | Out-Null
      break
    } catch {
      if ((Get-Date) -gt $deadline) {
        throw "$url is not responding; start the cluster with dev_cluster.ps1"
      }
      Start-Sleep -Seconds 1
    }
  }
}

Write-Host "Seeding the teams and the qualification schedule on the hub..."
$seedBody = @{
  teams = $Teams
  matchesPerTeam = $MatchesPerTeam
  fieldAssignment = $FieldAssignment
  minTurnaroundSec = $MinTurnaroundSec
  conf1Name = $Conf1Name
  conf1Short = $Conf1Short
  conf2Name = $Conf2Name
  conf2Short = $Conf2Short
}
if ($Roster) {
  $seedBody.roster = Get-Content -Raw $Roster
}
$seed = Invoke-RestMethod -Method Post -Uri "$hub/api/dev/seed_event" -Body $seedBody
Write-Host ("  {0} teams, {1} matches ({2} field assignment), {3}% mixed-conference alliances, seed {4}" -f
  $seed.Teams, $seed.Matches, $FieldAssignment, $seed.MixedAlliancePercent, $seed.Seed)

Write-Host "Playing qualifications on both fields..."
Start-Sleep -Seconds 3
foreach ($node in $nodes) {
  Invoke-Dev "$node/api/dev/auto_simulate?intervalSec=$IntervalSec&type=qualification" | Out-Null
}
Wait-ForMatches "qualification"
foreach ($node in $nodes) {
  Invoke-Dev "$node/api/dev/auto_simulate?intervalSec=0" | Out-Null
}

Write-Host "Running alliance selection on the hub..."
$selection = Invoke-Dev "$hub/api/dev/auto_alliance_selection"
Write-Host ("  {0} alliances, {1} playoff matches" -f $selection.Alliances, $selection.PlayoffMatches)

Write-Host "Playing the playoffs..."
Start-Sleep -Seconds 3
foreach ($node in $nodes) {
  Invoke-Dev "$node/api/dev/auto_simulate?intervalSec=$IntervalSec&type=playoff" | Out-Null
}
$deadline = (Get-Date).AddMinutes($TimeoutMin)
while ((Get-Date) -lt $deadline) {
  $awards = Invoke-RestMethod -Uri "$hub/api/matches/playoff"
  $unplayed = @($awards | Where-Object { -not $_.Result -and $_.Red1 -gt 0 -and $_.Blue1 -gt 0 }).Count
  $status = Invoke-RestMethod -Uri "$hub/api/dev/status"
  Write-Host ("  playable unplayed playoff matches: {0}" -f $unplayed)
  if ($unplayed -eq 0) {
    Start-Sleep -Seconds 5
    $again = Invoke-RestMethod -Uri "$hub/api/matches/playoff"
    if (@($again | Where-Object { -not $_.Result -and $_.Red1 -gt 0 -and $_.Blue1 -gt 0 }).Count -eq 0) {
      break
    }
  }
  Start-Sleep -Seconds 5
}
foreach ($node in $nodes) {
  Invoke-Dev "$node/api/dev/auto_simulate?intervalSec=0" | Out-Null
}
Write-Host "Done. See the bracket at $hub/displays/bracket?displayId=100&conference=all and awards at $hub/setup/awards"
