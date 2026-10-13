# Resumen Completo de Configuración — RyuNET CTR Online

Este documento reúne todas las configuraciones, servicios, puertos y rutas implementadas para el servidor **RyuNET Demonware (Crash Team Racing Nitro-Fueled PS4/PC)**.

---

## 1. Servicio Systemd (`onlinectr-port`)

El servidor corre como un servicio persistente administrado por systemd.

- **Archivo de configuración:** `/etc/systemd/system/onlinectr-port.service`
- **Directorio de trabajo:** `/mnt/extra/CTR-ONLINE/ryunet-demonware-ctr`
- **Binario en ejecución:** `/mnt/extra/CTR-ONLINE/ryunet-demonware-ctr/bin/server-ps4-linux-arm64`
- **Variables de entorno:** `/mnt/extra/CTR-ONLINE/ryunet-demonware-ctr/.env`
- **Usuario de ejecución:** `ubuntu:ubuntu`

### Comandos de control

```bash
# Ver estado en tiempo real
sudo systemctl status onlinectr-port

# Ver logs en vivo
sudo journalctl -u onlinectr-port -f

# Reiniciar o detener
sudo systemctl restart onlinectr-port
sudo systemctl stop onlinectr-port
```

---

## 2. Puertos y Firewall (UFW)

Todos los puertos requeridos para el juego, autenticación y estadísticas están habilitados:

| Puerto / Protocolo | Función | Uso en CTR / Demonware |
| :--- | :--- | :--- |
| **`8080/tcp`** | Auth HTTP | Para emuladores (shadPS4) y mods sin TLS |
| **`8460/tcp`** | Auth HTTPS | Autenticación oficial cifrada Demonware |
| **`3074 - 3080/tcp`** | Lobby Demonware | Matchmaking, salas, mensajería y presencia |
| **`3074/udp`** | NAT Discovery / STUN | Detección de IP y emparejamiento P2P |
| **`8093/tcp`** | Dashboard Stats API | Métricas internas (`/api/stats`, `/healthz`) |
| **`80/tcp`** | HTTP Web | Dashboard web público (redirige a HTTPS) |
| **`443/tcp`** | HTTPS Web | Dashboard web público con SSL Let's Encrypt |

---

## 3. Dominio y Dashboard Web (`octr.ryu7w7.xyz`)

Para evitar que el dominio redirigiera a otros sitios del servidor (`panel.ryu7w7.xyz`):

1. **Nginx Virtual Host:** `/etc/nginx/sites-available/octr.ryu7w7.xyz`
   - Escucha en puertos `80` y `443` exclusivamente para `octr.ryu7w7.xyz`.
   - Proxy seguro a `/api/stats` en `http://127.0.0.1:8093/api/stats?key=...` sin exponer la clave internamente.
2. **Dashboard de Estado en Tiempo Real:** `/var/www/octr.ryu7w7.xyz/index.html`
   - Visualización web interactiva con tema oscuro.
   - Muestra jugadores en línea, salas activas, uptime y peticiones atendidas en vivo cada 4 segundos.
3. **Certificado SSL:**
   - Certificado Let's Encrypt activo y con renovación automática configurada vía Certbot.

---

## 4. Configuración para Jugadores (Clientes)

### En PC (`bbport.ini`)
```ini
[Online]
online=1
online_server=octr.ryu7w7.xyz
```

### En emulador shadPS4 (`host_overrides.json`)
```json
"http://octr.ryu7w7.xyz:8080"
```

---

## 5. Servicios Preexistentes Preservados

Durante toda la intervención se mantuvo la integridad total de los servicios que ya estaban corriendo en tu servidor:
- **`yuki.service`** (Node.js / PM2 en puerto `9000` y `9004`): Intacto y activo.
- **`gasmox_srv.service`** (Servidor de juego UDP `5727`): Intacto y activo.
- **Pterodactyl Panel, AquaDX, Radio, etc.** en Nginx: Intactos y funcionando.
