#!/usr/bin/env bash
set -e

cd "$(dirname "$0")/.."

echo "===================================================="
echo " Compilando RyuNET Demonware Server"
echo "===================================================="

mkdir -p bin

echo "[1/3] Compilando para Linux (nativo)..."
go build -o bin/server-ps4 ./server

echo "[2/3] Compilando para Linux ARM64..."
GOOS=linux GOARCH=arm64 go build -o bin/server-ps4-linux-arm64 ./server

echo "[3/3] Compilando para Windows..."
GOOS=windows GOARCH=amd64 go build -o bin/server-ps4.exe ./server

echo "===================================================="
echo " Compilacion completada con exito en ./bin/"
echo "===================================================="
ls -lh bin/
