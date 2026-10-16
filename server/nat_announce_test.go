package main

import (
	"net"
	"testing"
)

// A configured server address is announced as is; RYUNET_HOST=0.0.0.0 announces, to each
// player, the address of this machine that reaches them (a LAN or VPN server like Radmin).
func TestNATAnnouncedServerAddress(t *testing.T) {
	public := net.IPv4(165, 1, 125, 122).To4()
	if got := announcedIP(public, net.IPv4(26, 1, 2, 3)); !got.Equal(public) {
		t.Fatalf("configured address replaced: %v", got)
	}

	any := resolveHostIPv4("0.0.0.0")
	if any == nil || !any.IsUnspecified() {
		t.Fatalf("RYUNET_HOST=0.0.0.0 resolves to %v", any)
	}
	local := announcedIP(any, net.IPv4(127, 0, 0, 1))
	if !local.Equal(net.IPv4(127, 0, 0, 1)) || len(local) != 4 {
		t.Fatalf("a local player was announced %v, want 127.0.0.1 (4 bytes)", local)
	}
}
