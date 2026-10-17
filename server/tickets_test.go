package main

import (
	"encoding/binary"
	"testing"
	"time"
)

func issuedAtTicket(fill byte, at time.Time) []byte {
	t := testTicket(fill)
	binary.LittleEndian.PutUint32(t[13:], uint32(at.Unix()))
	return t
}

func resetIssued(t *testing.T) {
	issuedMu.Lock()
	issuedTickets, issuedOrder = map[[8]byte]issuedTicket{}, nil
	issuedMu.Unlock()
	t.Cleanup(func() {
		issuedMu.Lock()
		issuedTickets, issuedOrder = map[[8]byte]issuedTicket{}, nil
		issuedMu.Unlock()
	})
}

// Pruning drops the tickets past the identity grace, oldest first, and keeps the rest
// readable.
func TestRememberTicketPrunesOldest(t *testing.T) {
	resetIssued(t)
	now := time.Now()
	old := issuedAtTicket(0x01, now.Add(-ticketWindow-issuedIdentityGrace-time.Minute))
	kept := issuedAtTicket(0x02, now.Add(-time.Hour))
	rememberTicket(old, playerID{PID: 1}, now, ticketWindow)
	rememberTicket(kept, playerID{PID: 2}, now, ticketWindow)
	fresh := issuedAtTicket(0x03, now)
	rememberTicket(fresh, playerID{PID: 3}, now, ticketWindow)

	if _, ok := issuedIdentity(old); ok {
		t.Fatal("ticket past the grace still remembered")
	}
	for _, tk := range [][]byte{kept, fresh} {
		if _, ok := issuedIdentity(tk); !ok {
			t.Fatalf("ticket %x lost", tk[33])
		}
	}
	if len(issuedOrder) != 2 || len(issuedTickets) != 2 {
		t.Fatalf("queue %d, map %d: want 2 and 2", len(issuedOrder), len(issuedTickets))
	}
}

// The same tag issued again must survive the expiry of its first issue.
func TestRememberTicketReissuedTagKept(t *testing.T) {
	resetIssued(t)
	now := time.Now()
	first := issuedAtTicket(0x05, now.Add(-ticketWindow-issuedIdentityGrace-time.Minute))
	again := issuedAtTicket(0x05, now)
	rememberTicket(first, playerID{PID: 5}, now, ticketWindow)
	rememberTicket(again, playerID{PID: 5}, now, ticketWindow)
	rememberTicket(issuedAtTicket(0x06, now), playerID{PID: 6}, now, ticketWindow)
	if _, ok := issuedIdentity(again); !ok {
		t.Fatal("reissued ticket dropped with its first issue")
	}
}

// The lobby's fallback sees only the tickets of the last window, newest first.
func TestRecentIssuedTickets(t *testing.T) {
	resetIssued(t)
	now := time.Now()
	rememberTicket(issuedAtTicket(0x07, now.Add(-time.Hour)), playerID{PID: 7}, now, ticketWindow)
	rememberTicket(issuedAtTicket(0x08, now.Add(-time.Minute)), playerID{PID: 8}, now, ticketWindow)
	rememberTicket(issuedAtTicket(0x09, now), playerID{PID: 9}, now, ticketWindow)

	tags, tickets := recentIssuedTickets(now, ticketWindow)
	if len(tickets) != 2 || tickets[0][33] != 0x09 || tickets[1][33] != 0x08 {
		t.Fatalf("got %d tickets, want 0x09 then 0x08", len(tickets))
	}
	if tags[0] != ticketTag(tickets[0]) {
		t.Fatal("tag does not match its ticket")
	}
}
