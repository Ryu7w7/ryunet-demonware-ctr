package main

// Tunnel for players who cannot reach each other directly (strict or carrier NAT).
//
// The game's NAT traversal (nat.go, 0x0A..0x0D) carries the peers' addresses inside signed
// packets and answers to them, so a relay address the game is handed (ctr_relay.go) is only
// tried as one more local address of the host: replies still go to the signed addresses, and
// players behind two strict NATs never connect (production 2026-10-16: parties and public
// rooms releasing "failed reservations" every second, relay pairs going quiet).
//
// The PC port (runtime_net.c) tunnels underneath the game instead, invisibly to it. Its game
// socket keeps a path to this UDP port open (tunnelKeepalive every 15 s), and while a peer has
// not proven that direct traffic works both ways, every packet to that peer is also sent here
// wrapped:
//
//	client -> server  "RYUTN1" flags dstIP[4] dstPort[2 BE] payload
//	server -> client  "RYUTN1" flags srcIP[4] srcPort[2 BE] payload
//
// The server only delivers to an address that itself sent a keepalive or a tunnel packet
// (so clients without the tunnel never receive one), from this port, which that client's NAT
// lets in. src is the sender's address as seen here: the same address its game advertised (it
// learned it from this port), so the receiving port hands the payload to its game as coming
// straight from that peer, and nothing the game signed changes. flags bit 0 is the sender
// saying it receives the destination's direct packets; the ports stop tunnelling once both
// sides say so.

import (
	"bytes"
	"encoding/binary"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

var (
	tunnelMagic     = []byte("RYUTN1")
	tunnelKeepalive = []byte("RYUKA1")
)

const (
	tunnelHeaderLen = 13              // magic + flags + ip + port
	tunnelClientTTL = 90 * time.Second // a client is reachable this long after its last packet
	tunnelMaxPPS    = 600              // per sender, packets a second
	tunnelMaxPeers  = 20000
)

type tunnelClient struct {
	seen        time.Time
	windowStart time.Time
	inWindow    int
}

var (
	tunnelMu      sync.Mutex
	tunnelClients = map[string]*tunnelClient{} // "ip:port" -> client
	tunnelPairs   = map[string]time.Time{}     // "src>dst" -> last time logged

	tunnelPackets atomic.Int64
	tunnelBytes   atomic.Int64
	tunnelDropped atomic.Int64
)

// handleTunnel takes a keepalive or tunnel packet. It returns false for any other packet,
// which nat.go goes on to handle as before.
func handleTunnel(pc net.PacketConn, p []byte, from *net.UDPAddr, now time.Time) bool {
	switch {
	case bytes.HasPrefix(p, tunnelKeepalive):
		tunnelMu.Lock()
		tunnelSeen(from, now)
		tunnelMu.Unlock()
		return true
	case !bytes.HasPrefix(p, tunnelMagic):
		return false
	}
	if len(p) <= tunnelHeaderLen {
		return true
	}
	dst := &net.UDPAddr{IP: net.IPv4(p[7], p[8], p[9], p[10]).To4(), Port: int(binary.BigEndian.Uint16(p[11:13]))}
	fromKey, dstKey := from.String(), dst.String()

	tunnelMu.Lock()
	c := tunnelSeen(from, now)
	if now.Sub(c.windowStart) >= time.Second {
		c.windowStart, c.inWindow = now, 0
	}
	c.inWindow++
	target, ok := tunnelClients[dstKey]
	deliver := c.inWindow <= tunnelMaxPPS && ok && now.Sub(target.seen) < tunnelClientTTL && dstKey != fromKey
	logPair := false
	if deliver {
		pair := fromKey + ">" + dstKey
		if last, seen := tunnelPairs[pair]; !seen || now.Sub(last) > 10*time.Minute {
			if len(tunnelPairs) >= tunnelMaxPeers {
				tunnelPairs = map[string]time.Time{}
			}
			tunnelPairs[pair] = now
			logPair = true
		}
	}
	tunnelMu.Unlock()

	if !deliver {
		tunnelDropped.Add(1)
		return true
	}
	if logPair {
		log.Printf("[RyuNET Tunnel] %s -> %s: relaying their game traffic (no direct path)", from, dst)
	}
	out := make([]byte, len(p))
	copy(out, p[:7])
	copy(out[7:11], from.IP.To4())
	binary.BigEndian.PutUint16(out[11:13], uint16(from.Port))
	copy(out[tunnelHeaderLen:], p[tunnelHeaderLen:])
	if _, err := pc.WriteTo(out, dst); err == nil {
		tunnelPackets.Add(1)
		tunnelBytes.Add(int64(len(out)))
	}
	return true
}

// tunnelSeen records a packet from a tunnel-capable client. Holds tunnelMu.
func tunnelSeen(from *net.UDPAddr, now time.Time) *tunnelClient {
	key := from.String()
	c, ok := tunnelClients[key]
	if !ok {
		if len(tunnelClients) >= tunnelMaxPeers {
			tunnelPrune(now)
		}
		c = &tunnelClient{windowStart: now}
		tunnelClients[key] = c
	}
	c.seen = now
	return c
}

// tunnelPrune drops clients gone quiet. Holds tunnelMu.
func tunnelPrune(now time.Time) {
	for key, c := range tunnelClients {
		if now.Sub(c.seen) >= tunnelClientTTL {
			delete(tunnelClients, key)
		}
	}
}

// tunnelReporter prunes the registry and logs what the tunnel carried, every 5 minutes.
func tunnelReporter() {
	for range time.Tick(5 * time.Minute) {
		now := time.Now()
		tunnelMu.Lock()
		tunnelPrune(now)
		clients := len(tunnelClients)
		tunnelMu.Unlock()
		packets, bytes, dropped := tunnelPackets.Swap(0), tunnelBytes.Swap(0), tunnelDropped.Swap(0)
		if packets > 0 || dropped > 0 {
			log.Printf("[RyuNET Tunnel] last 5 min: %d packets (%d KB) relayed, %d undeliverable; %d clients with the tunnel",
				packets, bytes/1024, dropped, clients)
		}
	}
}
