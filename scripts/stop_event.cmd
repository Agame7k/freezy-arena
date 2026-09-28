@echo off
rem Copyright 2026 Team 254. All Rights Reserved.
rem
rem Double-click to stop the hub and field nodes started by start_event.cmd or start_event.ps1.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0start_event.ps1" -Stop
echo.
echo The test event is stopped.
pause
