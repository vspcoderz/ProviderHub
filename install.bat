@echo off
rem Provider Hub installer for Windows.
rem Builds ph.exe and installs it, then adds it to PATH.
rem
rem Usage:
rem   install.bat                 install to %LOCALAPPDATA%\ProviderHub\bin
rem   set PREFIX=D:\tools & install.bat   custom install dir
rem   powershell -c "irm https://raw.githubusercontent.com/vspcoderz/ProviderHub/main/install.bat -OutFile %TEMP%\ph-install.bat; & %TEMP%\ph-install.bat"
rem
setlocal EnableDelayedExpansion

set "ROOT=%~dp0"

rem If no source tree (piped from the web), fetch the repo first.
if not exist "%ROOT%go.mod" (
  echo ==^> Fetching provider-hub source from https://github.com/vspcoderz/ProviderHub ^(main^)
  where git >nul 2>nul
  if errorlevel 1 (
    echo git not found - install Git, or clone the repo and run install.bat. 1>&2
    exit /b 1
  )
  set "TMP=%TEMP%\provider-hub-install"
  if exist "%TEMP%\provider-hub-install" rmdir /s /q "%TEMP%\provider-hub-install"
  git clone -q --depth 1 --branch main https://github.com/vspcoderz/ProviderHub.git "%TEMP%\provider-hub-install"
  if errorlevel 1 exit /b 1
  set "ROOT=%TEMP%\provider-hub-install\"
)

rem --- 1. Pick install dir ---------------------------------------------------
if "%PREFIX%"=="" set "PREFIX=%LOCALAPPDATA%\ProviderHub"
set "DEST=%PREFIX%\bin"
if not exist "%DEST%" mkdir "%DEST%"

echo ==^> Building ph.exe
pushd "%ROOT%"
go build -o ph.exe .\cmd\ph
if errorlevel 1 (
  echo Build failed. Is Go installed and on PATH? 1>&2
  exit /b 1
)
popd

echo ==^> Installing to %DEST%
copy /y "%ROOT%ph.exe" "%DEST%\ph.exe" >nul
del "%ROOT%ph.exe"

rem --- 2. Add to PATH ---------------------------------------------------------
echo "%PATH%" | find /i "%DEST%" >nul
if errorlevel 1 (
  echo ==^> Adding %DEST% to user PATH
  setx PATH "%DEST%;%PATH%"
  echo    Note: open a NEW terminal for PATH changes to take effect.
) else (
  echo ==^> %DEST% already on PATH
)

rem --- 3. Set up harness wrappers ---------------------------------------------
if exist "%DEST%\ph.exe" (
  echo ==^> Setting up ph-claude, ph-codex, ph-pi, ph-opencode wrappers
  "%DEST%\ph.exe" hsi setup
)

echo.
echo Installed provider-hub to %DEST%\ph.exe
echo The wrappers are POSIX shell scripts - use them from Git Bash / WSL, or call:
echo   ph hsi run claude --provider ^<id^> ^<args^>
echo Next: ph add   -^> add your first provider
endlocal