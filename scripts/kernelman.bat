@echo off
rem kernelman launcher for Windows x64: runs bin\kernelman-windows-amd64.exe next to this file.
rem No runtime has to be installed on the host.
set "BIN=%~dp0bin\kernelman-windows-amd64.exe"
if not exist "%BIN%" (
  echo kernelman: binary not found: %BIN% 1>&2
  exit /b 2
)
"%BIN%" %*
exit /b %ERRORLEVEL%
