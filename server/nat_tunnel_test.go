package main

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

// tunnelServer runs handleTunnel on a loopback UDP socket, as serveNAT does.
func tunnelServer(t *testing.T) *net.UDPConn {
	t.Helper()
	tunnelMu.Lock()
	tunnelClients = map[string]*tunnelClient{}
	tunnelPairs = map[string]time.Time{}
	tunnelMu.Unlock()
	srv, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := srv.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if !handleTunnel(srv, buf[:n], from, time.Now()) {
				t.Errorf("packet %q not taken as tunnel traffic", buf[:n])
			}
		}
	}()
	return srv
}

func tunnelPeer(t *testing.T) *net.UDPConn {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func wrapFor(dst *net.UDPAddr, flags byte, payload []byte) []byte {
	p := append([]byte{}, tunnelMagic...)
	p = append(p, flags)
	p = append(p, dst.IP.To4()...)
	p = binary.BigEndian.AppendUint16(p, uint16(dst.Port))
	return append(p, payload...)
}

func readWithin(c *net.UDPConn, d time.Duration) ([]byte, *net.UDPAddr, bool) {
	buf := make([]byte, 2048)
	_ = c.SetReadDeadline(time.Now().Add(d))
	n, from, err := c.ReadFromUDP(buf)
	if err != nil {
		return nil, nil, false
	}
	return buf[:n], from, true
}

func TestTunnelDeliversOnlyToClientsThatAnnouncedIt(t *testing.T) {
	srv := tunnelServer(t)
	server := srv.LocalAddr().(*net.UDPAddr)
	a, b := tunnelPeer(t), tunnelPeer(t)
	aAddr, bAddr := a.LocalAddr().(*net.UDPAddr), b.LocalAddr().(*net.UDPAddr)
	payload := []byte{0x0C, 0x02, 0x00, 0xDE, 0xAD}

	// b never spoke to the server (a client without the tunnel): nothing reaches it.
	if _, err := a.WriteToUDP(wrapFor(bAddr, 0, payload), server); err != nil {
		t.Fatal(err)
	}
	if p, _, ok := readWithin(b, 200*time.Millisecond); ok {
		t.Fatalf("a client without the tunnel got %q", p)
	}

	// After b's keepalive the packet arrives from the server, naming a as the sender.
	if _, err := b.WriteToUDP(tunnelKeepalive, server); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := a.WriteToUDP(wrapFor(bAddr, 1, payload), server); err != nil {
		t.Fatal(err)
	}
	p, from, ok := readWithin(b, time.Second)
	if !ok {
		t.Fatal("tunnelled packet not delivered")
	}
	if from.Port != server.Port {
		t.Fatalf("delivered from %s, want the server %s", from, server)
	}
	if !bytes.HasPrefix(p, tunnelMagic) || p[6] != 1 {
		t.Fatalf("header %X: want the magic and flags 1", p[:7])
	}
	src := &net.UDPAddr{IP: net.IPv4(p[7], p[8], p[9], p[10]), Port: int(binary.BigEndian.Uint16(p[11:13]))}
	if !src.IP.Equal(aAddr.IP) || src.Port != aAddr.Port {
		t.Fatalf("source %s, want a's address %s", src, aAddr)
	}
	if !bytes.Equal(p[tunnelHeaderLen:], payload) {
		t.Fatalf("payload %X, want %X", p[tunnelHeaderLen:], payload)
	}

	// And back: a is known from its own tunnel packet.
	if _, err := b.WriteToUDP(wrapFor(aAddr, 0, []byte{7}), server); err != nil {
		t.Fatal(err)
	}
	if p, _, ok := readWithin(a, time.Second); !ok || p[len(p)-1] != 7 {
		t.Fatalf("reply not delivered: %X", p)
	}
}

func TestTunnelLeavesOtherPacketsToTheNATServer(t *testing.T) {
	for _, p := range [][]byte{{0x0A, 2, 0}, {20, 2, 0}, []byte("RYUT")} {
		if handleTunnel(nil, p, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, time.Now()) {
			t.Errorf("%X taken as tunnel traffic", p)
		}
	}
}

func TestTunnelRateLimitsASender(t *testing.T) {
	tunnelMu.Lock()
	tunnelClients = map[string]*tunnelClient{}
	tunnelMu.Unlock()
	srv := tunnelPeer(t)
	dst := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40999}
	from := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 40998}
	now := time.Now()
	handleTunnel(srv, tunnelKeepalive, dst, now)
	before := tunnelPackets.Load()
	for i := 0; i < tunnelMaxPPS+50; i++ {
		handleTunnel(srv, wrapFor(dst, 0, []byte{1}), from, now)
	}
	if sent := tunnelPackets.Load() - before; sent != tunnelMaxPPS {
		t.Fatalf("relayed %d packets in one second, want %d", sent, tunnelMaxPPS)
	}
}
