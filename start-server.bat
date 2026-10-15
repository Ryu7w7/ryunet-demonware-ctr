@echo off
setlocal
cd /d "%~dp0"

echo ====================================================
echo   Servidor RyuNET Demonware (CTR Nitro-Fueled)
echo   Creditos: Nextendo Network (Demonware backend)
echo ====================================================

if not exist bin mkdir bin

rem Recompila siempre que Go este instalado, para no arrancar un binario viejo.
where go >nul 2>nul
if %errorlevel%==0 (
    echo [RyuNET] Compilando bin\server-ps4.exe...
    go build -o bin\server-ps4.exe .\server
    if errorlevel 1 (
        echo [RyuNET] ERROR: la compilacion fallo, no se arranca el servidor.
        pause
        exit /b 1
    )
) else if not exist bin\server-ps4.exe (
    echo [RyuNET] ERROR: no hay bin\server-ps4.exe y Go no esta instalado.
    pause
    exit /b 1
)

echo [RyuNET] Iniciando servidor...
bin\server-ps4.exe
pause
