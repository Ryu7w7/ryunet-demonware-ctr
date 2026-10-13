@echo off
setlocal
cd /d "%~dp0"

echo ====================================================
echo   Servidor RyuNET Demonware (CTR Nitro-Fueled)
echo   Creditos: Nextendo Network (Demonware backend)
echo ====================================================

if not exist bin\server-ps4.exe (
    echo [RyuNET] Compilando bin\server-ps4.exe...
    if not exist bin mkdir bin
    go build -o bin\server-ps4.exe .\server
)

echo [RyuNET] Iniciando servidor...
bin\server-ps4.exe
pause
