package main

import "testing"

func noticesTo(out []ctrNotice, conn *lobbyConn) []ctrNotice {
	var got []ctrNotice
	for _, n := range out {
		if n.conn == conn {
			got = append(got, n)
		}
	}
	return got
}

// With CTR_PARTY_NOTIFY_MEMBERS=1 a joining party's member gets the same join event
// as its leader, carrying the leader's search ID, once the host has acknowledged.
func TestCTRPartyMemberToldToJoin(t *testing.T) {
	t.Setenv("CTR_PARTY_NOTIFY_MEMBERS", "1")
	m := ctrMatchmaker{rooms: map[uint64]*ctrRoom{}}
	r := fillTestRoom(t, &m, 7000, 3)

	party := testCTRSearch(t, 8000, 0)
	party.doc.Members = []uint64{8000, 8001}
	party.seats = 2
	member := &lobbyConn{n: 8001, player: &playerID{PID: 8001}}
	rememberConn(member)
	t.Cleanup(func() { forgetConn(member) })

	if out := m.enqueue(party); len(noticesTo(out, member)) != 0 {
		t.Fatal("member told before the host acknowledged the seats")
	}
	h, _ := r.documents()
	ok, out := m.syncDoc(r.host.conn, r.id, r.version, h)
	if !ok {
		t.Fatal("host sync rejected")
	}
	got := noticesTo(out, member)
	if len(got) != 1 || got[0].event != ctrJoinLobby || got[0].ids[0] != party.id || got[0].ids[1] != r.id {
		t.Fatalf("member join notice = %+v, want one 2001 with leader search %d room %d", got, party.id, r.id)
	}
	if len(noticesTo(out, party.conn)) != 1 {
		t.Fatal("leader no longer told")
	}
	if _, out := m.syncDoc(r.host.conn, r.id, r.version, h); len(noticesTo(out, member)) != 0 {
		t.Fatal("member told twice")
	}
}

// The members of the party that HOSTS the new room are told to join it once the host
// has created it, never told to create one themselves.
func TestCTRHostPartyMembersToldToJoin(t *testing.T) {
	t.Setenv("CTR_PARTY_NOTIFY_MEMBERS", "1")
	m := ctrMatchmaker{rooms: map[uint64]*ctrRoom{}}
	leader := testCTRSearch(t, 9000, 0)
	leader.doc.Members = []uint64{9000, 9001}
	leader.seats = 2
	member := &lobbyConn{n: 9001, player: &playerID{PID: 9001}}
	rememberConn(member)
	t.Cleanup(func() { forgetConn(member) })

	out := m.enqueue(leader)
	if len(out) != 1 || out[0].event != ctrCreateLobby || out[0].conn != leader.conn {
		t.Fatalf("party did not get its own room: %+v", out)
	}
	if len(noticesTo(out, member)) != 0 {
		t.Fatal("member told to create a lobby")
	}
	r := m.rooms[out[0].ids[1]]
	h, _ := r.documents()
	_, out = m.syncDoc(leader.conn, r.id, r.version, h)
	got := noticesTo(out, member)
	if len(got) != 1 || got[0].event != ctrJoinLobby || got[0].ids[1] != r.id {
		t.Fatalf("host's party member not told to join: %+v", got)
	}
}

func TestCTRPartyMemberNotToldWhenDisabled(t *testing.T) {
	t.Setenv("CTR_PARTY_NOTIFY_MEMBERS", "0")
	m := ctrMatchmaker{rooms: map[uint64]*ctrRoom{}}
	leader := testCTRSearch(t, 9100, 0)
	leader.doc.Members = []uint64{9100, 9101}
	leader.seats = 2
	member := &lobbyConn{n: 9101, player: &playerID{PID: 9101}}
	rememberConn(member)
	t.Cleanup(func() { forgetConn(member) })
	out := m.enqueue(leader)
	r := m.rooms[out[0].ids[1]]
	h, _ := r.documents()
	if _, out := m.syncDoc(leader.conn, r.id, r.version, h); len(noticesTo(out, member)) != 0 {
		t.Fatal("member told while CTR_PARTY_NOTIFY_MEMBERS=0")
	}
}

func TestCTRPartyNotifyOnByDefault(t *testing.T) {
	t.Setenv("CTR_PARTY_NOTIFY_MEMBERS", "")
	if !partyNotifyEnabled() {
		t.Fatal("party member notification must be on by default")
	}
}

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
