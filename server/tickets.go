package main

import (
	"bytes"
	"encoding/binary"
	"sync"
	"time"
)

// Tickets admit only a fresh connection, and the auth handler, lobby and
// Umbrella that read them run in this process, so they are kept in memory by
// the ticket's 8-byte session tag. This eliminates disk I/O and prevents thousands
// of ticket/session files from accumulating on disk.
type issuedTicket struct {
	ticket []byte
	id     playerID
}

var (
	issuedMu      sync.RWMutex
	issuedTickets = map[[8]byte]issuedTicket{}
)

// ticketWindow: tickets must be used to connect within this duration.
const ticketWindow = 120 * time.Second

// issuedIdentityGrace: identity stays readable this long for reconnects or Umbrella token exchanges.
const issuedIdentityGrace = 24 * time.Hour

func ticketTag(ticket []byte) (tag [8]byte) {
	if len(ticket) >= 41 {
		copy(tag[:], ticket[33:41])
	}
	return tag
}

func ticketIssuedAt(ticket []byte) time.Time {
	if len(ticket) >= 17 {
		return time.Unix(int64(binary.LittleEndian.Uint32(ticket[13:17])), 0)
	}
	return time.Time{}
}

// rememberTicket records a ticket issued to id and purges old ones from memory.
func rememberTicket(ticket []byte, id playerID, now time.Time, window time.Duration) {
	issuedMu.Lock()
	defer issuedMu.Unlock()
	for tag, t := range issuedTickets {
		if now.Sub(ticketIssuedAt(t.ticket)) >= window+issuedIdentityGrace {
			delete(issuedTickets, tag)
		}
	}
	issuedTickets[ticketTag(ticket)] = issuedTicket{ticket: append([]byte(nil), ticket...), id: id}
}

// issuedTicketPlayer checks if the ticket was issued by this server within window and has valid identity.
func issuedTicketPlayer(ticket []byte, now time.Time, window time.Duration) (*playerID, bool) {
	if len(ticket) != 128 || binary.LittleEndian.Uint32(ticket) != ticketMagic {
		return nil, false
	}
	issued := ticketIssuedAt(ticket)
	if issued.After(now) || now.Sub(issued) >= window {
		return nil, false
	}
	return issuedIdentity(ticket)
}

// issuedIdentity returns the identity a ticket was issued to.
func issuedIdentity(ticket []byte) (*playerID, bool) {
	if len(ticket) != 128 {
		return nil, false
	}
	issuedMu.RLock()
	t, ok := issuedTickets[ticketTag(ticket)]
	issuedMu.RUnlock()
	if !ok || !bytes.Equal(t.ticket, ticket) || t.id.PID == 0 {
		return nil, false
	}
	id := t.id
	id.Title = binary.LittleEndian.Uint32(ticket[5:9])
	return &id, true
}

func issueTestTicket(ticket []byte, id playerID) {
	rememberTicket(ticket, id, time.Now(), ticketWindow)
}
