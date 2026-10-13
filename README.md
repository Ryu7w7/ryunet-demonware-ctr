# RyuNET Demonware Server (CTR Nitro-Fueled PS4 / PC)

Servidor de juego independiente para **Crash Team Racing Nitro-Fueled** (títulos Demonware 5775 / 5760 en PS4 y PC/shadPS4), que implementa el stack de red Demonware completo de extremo a extremo en Go.

---

## Características

- **Autenticación (Auth)**:
  - Soporte HTTPS nativo en el puerto `8460` para clientes oficiales.
  - Soporte HTTP claro en el puerto `8080` (ideal para emuladores, shadPS4 y desarrollo local con `host_overrides.json` sin certificados TLS).
  - Cálculo de PID estable y compatible con perfiles locales o backend.
- **Lobby y Matchmaking (TCP 3074-3080)**:
  - Cifrado Demonware (HMAC-SHA1, HKDF, AES-128-CBC) y emulación de llamadas remotas (RMC).
  - Creación, búsqueda y unión a salas de carrera públicas y privadas.
  - Sincronización de estado de jugadores y presencia.
- **NAT & STUN Discovery (UDP 3074)**:
  - Detección de IP pública e introducción NAT entre jugadores para juego P2P.
- **Pit Stop y Economía (`bdEconomy` & `bdMarketplace`)**:
  - Tienda Pit Stop funcional con rotación de artículos y paquetes.
  - Monedas Wumpa: balance persistente, recompensas por carreras completadas y bonificaciones por fin de semana.
- **Grand Prix y Desafíos (`bdAchievements`)**:
  - Sistema de desafíos activos (diarios, semanales, temáticos) y Window Shopping.
- **Estadísticas y Clasificaciones (`bdStats`)**:
  - Registro de tiempos de pista y clasificaciones globales.
- **Almacenamiento de Objetos (`bdObjectStore`)**:
  - Servido de configuraciones de Beenox (`NetConfig_23_ps4_na.igx`, `MarketplaceConfig_2_ps4_na.igx`, etc.).
- **Dashboard de Monitoreo**:
  - Endpoint de estadísticas `/api/stats` y `/healthz` en el puerto `8093` con protección por token.

---

## Compilación e Inicio

### Requisitos
- **Go 1.23** o superior instalado en el sistema.

### 1. Compilación
En **Windows**:
```cmd
scripts\build.bat
```
En **Linux / VPS**:
```bash
chmod +x scripts/build.sh
./scripts/build.sh
```
O directamente con `go build`:
```bash
# Para tu sistema actual:
go build -o server-ps4.exe .

# Para Linux ARM64 (ej. VPS Ubuntu en ARM):
GOOS=linux GOARCH=arm64 go build -o server-ps4-linux-arm64 .
```

### 2. Configuración
Copia `example.env` a `.env` y ajusta según tu entorno:
```bash
cp example.env .env
```
Parámetros clave:
- `RYUNET_HOST`: IP pública accesible por los jugadores (en local usar `127.0.0.1`).
- `DEMONWARE_HTTP_PORT`: Puerto para autenticación sin cifrar (recomendado `8080` para PC/shadPS4).
- `LOBBY_PORTS`: Rango de puertos TCP para salas (por defecto `3074,3075,3076,3077,3078,3079,3080`).
- `NAT_PORT`: Puerto UDP para STUN/NAT (por defecto `3074`).

### 3. Ejecutar
- En **Windows**: Doble clic en `start-server.bat` o ejecuta `.\server-ps4.exe`.
- En **Linux / VPS**: `./server-ps4-linux-arm64` (o configúralo como servicio con systemd).

---

## Conectar el Juego (CTR PC / shadPS4)

En la carpeta del juego CTR:
1. Abre tu archivo de configuración `bbport.ini`.
2. Activa el modo online apuntando a la IP de tu servidor RyuNET:
   ```ini
   [Online]
   online=1
   online_server=TU_IP_O_127.0.0.1
   ```
3. Si juegas en red local o a través de Internet, asegúrate de que los puertos `8080`, `8460`, `3074-3080` (TCP) y `3074` (UDP) estén abiertos en tu firewall/router.

---

## Tests Unitarios

El proyecto cuenta con una suite completa de pruebas para todos los subsistemas dentro de `tests/`:

En **Windows**:
```cmd
scripts\test.bat
```
En **Linux / macOS**:
```bash
chmod +x scripts/test.sh
./scripts/test.sh
```

---

## Créditos y Agradecimientos

Este proyecto es una adaptación y mantenimiento independiente denominado **RyuNET**, construido a partir del excelente trabajo previo de ingeniería inversa de la comunidad:

- **[Nextendo Network](https://github.com/NextendoNetwork)**: Creadores de la arquitectura base del servidor Demonware en Go y del ecosistema de emulación de redes de consola.
- **[nx-mod](https://github.com/nx-mod)**: Creador del repositorio original y del motor de protocolos Demonware.
- **[CollectingW](https://github.com/CollectingW)**: Implementación e investigación original de la capa online para Crash Team Racing Nitro-Fueled (Title 5775).
- **RyuPC / RyuNET**: Limpieza, estandarización para PC / PS4, separación de módulos, correcciones en el handshake de autenticación y empaquetado portátil.

---

## Licencia

Este proyecto se distribuye con fines educativos y de preservación histórica. Consulta [LICENSE.md](LICENSE.md) para más detalles.
