package main

import "testing"

// Only the leader searches for a party, but every member connects to the lobby from
// its own connection and needs the same documents to do so.
func TestCTRPartyMemberGetsLobbyDocuments(t *testing.T) {
	m := ctrMatchmaker{rooms: map[uint64]*ctrRoom{}}
	r := fillTestRoom(t, &m, 3000, 4)

	party := testCTRSearch(t, 4000, 0)
	party.doc.Members = []uint64{4000, 4001}
	party.seats = 2
	if out := m.enqueue(party); len(out) != 1 || out[0].ids[0] != r.id {
		t.Fatal("party not placed in the open room")
	}

	member := &lobbyConn{n: 4001, player: &playerID{PID: 4001}}
	if _, b, ok := m.documents(member, r.id); !ok || b == "" {
		t.Fatal("party member refused the lobby documents its leader was placed in")
	}
	if _, _, ok := m.documents(party.conn, r.id); !ok {
		t.Fatal("leader refused its own lobby documents")
	}

	stranger := &lobbyConn{n: 9999, player: &playerID{PID: 9999}}
	if _, _, ok := m.documents(stranger, r.id); ok {
		t.Fatal("a player outside the room got its documents")
	}
	anonymous := &lobbyConn{n: 9998}
	if _, _, ok := m.documents(anonymous, r.id); ok {
		t.Fatal("a connection without a player got the documents")
	}
}

// A member who searched alone before joining the party must not keep that seat: it
// would be counted twice and its old room would expect a player who never comes.
func TestCTRPartySearchEvictsMembersOldSeats(t *testing.T) {
	m := ctrMatchmaker{rooms: map[uint64]*ctrRoom{}}
	r := fillTestRoom(t, &m, 5000, 3)

	solo := testCTRSearch(t, 6001, 0)
	m.enqueue(solo)
	if r.occupancy() != 4 {
		t.Fatalf("solo not placed: occupancy %d", r.occupancy())
	}

	party := testCTRSearch(t, 6000, 1) // a different playlist, so it cannot land in r
	party.doc.Members = []uint64{6000, 6001}
	party.seats = 2
	m.enqueue(party)
	if r.occupancy() != 3 {
		t.Fatalf("member's old seat kept: occupancy %d, want 3", r.occupancy())
	}
	for _, p := range r.players {
		if p.pid == 6001 {
			t.Fatal("member's old search still in the room")
		}
	}
}
