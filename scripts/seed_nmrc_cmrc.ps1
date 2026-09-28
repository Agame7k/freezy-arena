# Copyright 2026 Team 254. All Rights Reserved.
#
# Builds a two-conference event database for the Northern Minnesota Robotics Conference (NMRC) and the Central
# Minnesota Robotics Conference (CMRC): teams with their conferences, and a full qualification schedule assigned across
# two fields. By default the teams come from docs\nmrc_cmrc_teams.csv (the published member lists; check them against
# this season's rosters); -Placeholder uses numbered dummy teams instead.
#
#   .\scripts\seed_nmrc_cmrc.ps1                           # writes nmrc_cmrc.db
#   .\scripts\seed_nmrc_cmrc.ps1 -Placeholder              # dummy teams instead of the real rosters
#   .\scripts\seed_nmrc_cmrc.ps1 -Db dev_cluster\hub.db    # seed the dev cluster's hub database instead
#
# Then run it standalone:    .\cheesy-arena.exe -db nmrc_cmrc.db -role standalone
# or as a multi-field hub:   .\cheesy-arena.exe -db nmrc_cmrc.db -role hub -secret dev-secret -simulate

param(
  [string]$Db = "nmrc_cmrc.db",
  [string]$Roster = "docs\nmrc_cmrc_teams.csv",
  [switch]$Placeholder,
  [int]$TeamsPerConference = 24,
  [int]$MatchesPerTeam = 10,
  [int]$FirstTeam = 9001,
  [int]$Port = 18090
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
  if (Test-Path $Db) {
    throw "$Db already exists; delete it first or pass a different -Db"
  }
  go build -o cheesy-arena.exe .
  if ($LASTEXITCODE -ne 0) {
    throw "Build failed"
  }

  # Start a temporary hub on the new database, seed it through the dev API, and stop it again.
  $process = Start-Process -FilePath ".\cheesy-arena.exe" `
    -ArgumentList @("-port", $Port, "-db", $Db, "-role", "hub", "-secret", "dev-secret", "-simulate") `
    -PassThru -WindowStyle Hidden -RedirectStandardError "$Db.seed.log" -RedirectStandardOutput "$Db.seed.out.log"
  try {
    $deadline = (Get-Date).AddSeconds(30)
    while ($true) {
      try {
        Invoke-RestMethod -Uri "http://localhost:$Port/api/dev/status" | Out-Null
        break
      } catch {
        if ((Get-Date) -gt $deadline) {
          throw "The temporary instance didn't start; see $Db.seed.log"
        }
        Start-Sleep -Seconds 1
      }
    }
    $body = @{
      teams = 2 * $TeamsPerConference
      matchesPerTeam = $MatchesPerTeam
      firstTeam = $FirstTeam
      conf1Name = "NMRC"
      conf1Short = "NM"
      conf2Name = "CMRC"
      conf2Short = "CM"
      spacingSec = 240
    }
    if (-not $Placeholder) {
      $body.roster = Get-Content -Raw $Roster
    }
    $result = Invoke-RestMethod -Method Post -Uri "http://localhost:$Port/api/dev/seed_event" -Body $body
    $summary = ("Created {0} teams (NMRC and CMRC) and {1} qualification matches; {2}% of alliances mix " +
      "conferences (schedule seed {3}).") -f $result.Teams, $result.Matches, $result.MixedAlliancePercent, $result.Seed
    Write-Host $summary
  } finally {
    Stop-Process -Id $process.Id -Force
    Start-Sleep -Milliseconds 500
    Remove-Item "$Db.seed.log", "$Db.seed.out.log" -ErrorAction SilentlyContinue
  }
} finally {
  Pop-Location
}
Write-Host "Wrote $Db"
