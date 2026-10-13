#!/usr/bin/env bash
set -e

cd "$(dirname "$0")/.."

echo "===================================================="
echo " Ejecutando suite de pruebas de RyuNET Demonware"
echo "===================================================="

go test -v ./server

echo "===================================================="
echo " PRUEBAS EXITOSAS (100% aprobadas)"
echo "===================================================="
