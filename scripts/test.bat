@echo off
setlocal
cd /d "%~dp0\.."

echo ====================================================
echo  Ejecutando suite de pruebas de RyuNET Demonware
echo ====================================================

go test -v ./server
set TEST_EXIT=%errorlevel%

if %TEST_EXIT% EQU 0 (
    echo.
    echo ====================================================
    echo  PRUEBAS EXITOSAS: 100 por ciento aprobadas
    echo ====================================================
)
if %TEST_EXIT% NEQ 0 (
    echo.
    echo [ERROR] Algunas pruebas fallaron con codigo %TEST_EXIT%
)

exit /b %TEST_EXIT%
