package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testTicket(fill byte) []byte {
	t := make([]byte, 128)
	for i := range t {
		t[i] = fill
	}
	copy(t, []byte{0xDE, 0xAD, 0xBD, 0xEF})
	return t
}

// The 0x82's own ticket must come first, saved tickets after it and zero last, so
// that a match on the first candidate never touches the sessions directory.
func TestCandidatesOrderAndEarlyStop(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "saved.tkt"), testTicket(0x22), 0o644); err != nil {
		t.Fatal(err)
	}
	l := &lobbyConn{sessDir: dir}
	raw := append([]byte{0x01, 0x02}, testTicket(0x11)...)

	var names []string
	for c := range l.candidates(raw) {
		names = append(names, c.name)
	}
	if len(names) < 5 || !strings.HasPrefix(names[0], "ticket-0x82") || !strings.HasPrefix(names[1], "ticket-0x82") {
		t.Fatalf("0x82 ticket not tried first: %v", names)
	}
	last := names[len(names)-1]
	if last != "zero" {
		t.Fatalf("zero key not last: %v", names)
	}
	sawSaved := false
	for _, n := range names {
		if strings.HasPrefix(n, "saved.tkt") {
			sawSaved = true
		}
	}
	if !sawSaved {
		t.Fatalf("saved ticket missing: %v", names)
	}

	// The consumer stopping must stop the producer: nothing past the first key.
	got := 0
	for range l.candidates(raw) {
		got++
		break
	}
	if got != 1 {
		t.Fatalf("iteration did not stop, got %d", got)
	}
}
