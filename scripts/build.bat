@echo off
setlocal
cd /d "%~dp0\.."

echo ====================================================
echo  Compilando RyuNET Demonware Server
echo ====================================================

if not exist bin mkdir bin

echo [1/3] Compilando binario para Windows (bin\server-ps4.exe)...
set GOOS=windows
set GOARCH=amd64
go build -o bin\server-ps4.exe .\server
if %errorlevel% neq 0 (
    echo [ERROR] Fallo la compilacion para Windows.
    exit /b %errorlevel%
)

echo [2/3] Compilando binario para Linux ARM64 (bin\server-ps4-linux-arm64)...
set GOOS=linux
set GOARCH=arm64
go build -o bin\server-ps4-linux-arm64 .\server
if %errorlevel% neq 0 (
    echo [ERROR] Fallo la compilacion para Linux ARM64.
    exit /b %errorlevel%
)

echo [3/3] Compilando binario para Linux x86_64 (bin\server-ps4-linux-amd64)...
set GOOS=linux
set GOARCH=amd64
go build -o bin\server-ps4-linux-amd64 .\server
if %errorlevel% neq 0 (
    echo [ERROR] Fallo la compilacion para Linux AMD64.
    exit /b %errorlevel%
)

set GOOS=
set GOARCH=

echo.
echo ====================================================
echo  Compilacion exitosa. Binarios guardados en .\bin\
echo ====================================================
dir bin
pause
