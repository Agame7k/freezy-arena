@echo off
rem Copyright 2026 Team 254. All Rights Reserved.
rem
rem Double-click to start the hub and both field nodes on this computer for testing (see start_event.ps1). Works even
rem where PowerShell scripts are blocked. Options are passed on, e.g. "start_event.cmd -RunAll" or
rem "start_event.cmd -HubPort 9080". The servers keep running after this window closes; stop them with stop_event.cmd.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0start_event.ps1" %*
set status=%errorlevel%
if not "%status%"=="0" (
  echo.
  echo The test event did not start; see the message above.
)
echo.
pause
exit /b %status%
