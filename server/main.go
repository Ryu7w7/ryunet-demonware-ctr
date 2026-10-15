// ryunet-demonware — Demonware game server for CTR Nitro-Fueled (PS4)
// Proyecto: RyuNET
// Basado en la investigacion y codigo de Nextendo Network (Demonware server).
// Creditos a: Nextendo Network (https://github.com/NextendoNetwork)
//
// Un proceso sirve todo el stack online Demonware para CTR Nitro-Fueled:
//	auth        HTTP / HTTPS para autenticacion Demonware
//	lobby       TCP 3074 (+3075..3080), tareas remotas y matchmaking
//	NAT         UDP 3074: STUN / IP discovery e introducciones
//	dashboard   /api/stats en DASH_PORT (8093) para monitoreo
package main

import (
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var (
	// The server's IPv4 address as the players see it, announced in NAT
	// discovery replies.
	nextendoHost = envOr("RYUNET_HOST", envOr("NEXTENDO_HOST", "127.0.0.1"))
	ryunetHost   = nextendoHost
	authPort     = envOrInt("AUTH_PORT", 8460)
	// DEMONWARE_HTTP_PORT enables cleartext HTTP for shadPS4 localhost PoC (host_overrides.json -> http://127.0.0.1:8080)
	// When 0 or unset, only HTTPS is served.
	httpPort   = envOrInt("DEMONWARE_HTTP_PORT", 0)
	lobbyPorts = envOr("LOBBY_PORTS", "3074,3075,3076,3077,3078,3079,3080")
	natPort    = envOrInt("NAT_PORT", 3074)
	certFile   = resolveCertPath("CERT_FILE", "cert.pem")
	keyFile    = resolveCertPath("KEY_FILE", "key.pem")

	// Tickets and player identities written by auth, read back by the lobby.
	sessDir = envOr("RYUNET_SESSIONS", envOr("D3_SESSIONS", "sessions"))
	pubDir  = envOr("RYUNET_PUBFILES", envOr("D3_PUBFILES", "pubfiles"))

	// Raw captures (auth bodies, every lobby frame) under <dumps>/<start>/.
	// RYUNET_DUMPS=off disables them.
	dumpRoot = envOr("RYUNET_DUMPS", envOr("D3_DUMPS", "off"))
	// RYUNET_VERBOSE=1 logs every decrypted lobby message as a hex dump.
	verbose = os.Getenv("RYUNET_VERBOSE") == "1" || os.Getenv("D3_VERBOSE") == "1"

	runStamp = time.Now().Format("20060102-150405")
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func resolveCertPath(key, def string) string {
	val := envOr(key, def)
	if _, err := os.Stat(val); err == nil {
		return val
	}
	if _, err := os.Stat(filepath.Join("certs", val)); err == nil {
		return filepath.Join("certs", val)
	}
	return val
}

func resolveHostIPv4(host string) net.IP {
	if ip := net.ParseIP(host).To4(); ip != nil {
		return ip
	}
	ips, err := net.LookupIP(host)
	if err == nil {
		for _, ip := range ips {
			if v4 := ip.To4(); v4 != nil {
				return v4
			}
		}
	}
	return nil
}

func envOrInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// runDumpDir returns this run's capture directory for one component, "" when
// captures are off.
func runDumpDir(component string) string {
	if dumpRoot == "off" || dumpRoot == "0" {
		return ""
	}
	dir := filepath.Join(dumpRoot, runStamp, component)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[RyuNET] dumps %s: %v", dir, err)
		return ""
	}
	return dir
}

func loadEnvFile() {
	for _, path := range []string{".env", "ryunet.env", "nextendo.env"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if idx := strings.Index(line, "="); idx > 0 {
				key := strings.TrimSpace(line[:idx])
				val := strings.TrimSpace(line[idx+1:])
				// Remove surrounding quotes if present
				if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
					val = val[1 : len(val)-1]
				}
				if os.Getenv(key) == "" {
					os.Setenv(key, val)
				}
			}
		}
		log.Printf("[RyuNET] loaded env file %s", path)
		// Reload vars that were initialized at package init via envOr
		// (they were set before loadEnvFile, so refresh them)
		nextendoHost = envOr("RYUNET_HOST", envOr("NEXTENDO_HOST", "127.0.0.1"))
		ryunetHost = nextendoHost
		authPort = envOrInt("AUTH_PORT", 8460)
		httpPort = envOrInt("DEMONWARE_HTTP_PORT", 0)
		lobbyPorts = envOr("LOBBY_PORTS", "3074,3075,3076,3077,3078,3079,3080")
		natPort = envOrInt("NAT_PORT", 3074)
		certFile = resolveCertPath("CERT_FILE", "cert.pem")
		keyFile = resolveCertPath("KEY_FILE", "key.pem")
		sessDir = envOr("RYUNET_SESSIONS", envOr("D3_SESSIONS", "sessions"))
		pubDir = envOr("RYUNET_PUBFILES", envOr("D3_PUBFILES", "pubfiles"))
		dumpRoot = envOr("RYUNET_DUMPS", envOr("D3_DUMPS", "off"))
		verbose = os.Getenv("RYUNET_VERBOSE") == "1" || os.Getenv("D3_VERBOSE") == "1"
		logDebug = debugLevel()
		break
	}
}

func main() {
	log.SetOutput(os.Stdout)
	loadEnvFile()

	log.Println("====================================================")
	log.Println(" Servidor RyuNET Demonware (CTR Nitro-Fueled PS4)")
	log.Println(" Creditos: Nextendo Network (Demonware Server)")
	log.Println("====================================================")

	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		log.Fatalf("[RyuNET] sessions dir: %v", err)
	}
	loadGates()
	loadWallets()

	go startDashboard()
	startPresenceReporter()
	go serveNAT(natPort)
	startCTRRelay(envOrInt("CTR_RELAY_PORT_LOW", 0), envOrInt("CTR_RELAY_PORT_HIGH", 0), nextendoHost)
	go reapSessions()
	ctrMM.startReaper()
	startLobby()

	// PS4 PoC: also serve cleartext HTTP for shadPS4 host_overrides.json (http://127.0.0.1:8080)
	if httpPort != 0 {
		go func() {
			if err := serveAuthHTTP(httpPort); err != nil {
				log.Printf("[RyuNET Auth HTTP] stopped: %v", err)
			}
		}()
		log.Printf("[RyuNET Auth] HTTP also listening :%d for PS4/PC (lavender)", httpPort)
	}

	// HTTPS auth: if cert missing and HTTP fallback is active, don't fatal (localhost PoC)
	if _, err := os.Stat(certFile); err != nil {
		if _, err2 := os.Stat(keyFile); err2 != nil && httpPort != 0 {
			log.Printf("[RyuNET Auth] cert files missing (%s/%s) but DEMONWARE_HTTP_PORT=%d active - skipping HTTPS, using HTTP only", certFile, keyFile, httpPort)
			select {}
		}
	}
	if err := serveAuth(); err != nil {
		log.Fatalf("[RyuNET Auth] stopped: %v", err)
	}
}
