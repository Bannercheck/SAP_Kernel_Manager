@echo off
rem sapkernel launcher for Windows x64: runs bin\sapkernel-windows-amd64.exe next to this file.
rem No runtime has to be installed on the host.
set "BIN=%~dp0bin\sapkernel-windows-amd64.exe"
if not exist "%BIN%" (
  echo sapkernel: binary not found: %BIN% 1>&2
  exit /b 2
)
"%BIN%" %*
exit /b %ERRORLEVEL%
