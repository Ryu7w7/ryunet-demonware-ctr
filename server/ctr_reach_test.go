package main

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

// hostReports has the room's host sync a document giving these joiners these states.
func hostReports(t *testing.T, m *ctrMatchmaker, r *ctrRoom, states map[uint64]int) {
	t.Helper()
	doc := map[string]any{}
	for k, v := range r.hostDoc {
		doc[k] = v
	}
	st := map[string]int{strconv.FormatUint(r.host.pid, 10): 2}
	for pid, s := range states {
		st[strconv.FormatUint(pid, 10)] = s
	}
	doc["player_state"] = st
	raw, _ := json.Marshal(doc)
	if ok, _ := m.syncDoc(r.host.conn, r.id, r.version, string(raw)); !ok {
		t.Fatal("host sync rejected")
	}
}

func testParty(t *testing.T, leader uint64, members ...uint64) *ctrSearch {
	t.Helper()
	s := testCTRSearch(t, leader, 0)
	s.doc.Members = append([]uint64{leader}, members...)
	s.seats = len(s.doc.Members)
	return s
}

// The production case: a party placed with a host neither member can reach searches again.
func TestCTRPartyNotSentBackToHostItFailedWith(t *testing.T) {
	m := ctrMatchmaker{rooms: map[uint64]*ctrRoom{}}
	r := fillTestRoom(t, &m, 6000, 2)
	party := testParty(t, 7000, 7001)
	if out := m.enqueue(party); len(out) != 1 || out[0].ids[0] != r.id {
		t.Fatal("party not placed in the open room")
	}
	hostReports(t, &m, r, map[uint64]int{6001: 2, 7000: 3, 7001: 3})
	if r.occupancy() != 2 {
		t.Fatalf("failed party still holds seats: %d", r.occupancy())
	}

	out := m.enqueue(testParty(t, 7000, 7001))
	if len(out) != 1 || out[0].event != ctrCreateLobby || out[0].ids[1] == r.id {
		t.Fatalf("party sent back to the host it failed with: %+v", out)
	}
	if got := m.rooms[out[0].ids[1]]; got == nil || got.host.pid != 7000 {
		t.Fatal("the party should host its own lobby")
	}
	// Someone else still gets the first room.
	if out := m.enqueue(testCTRSearch(t, 8000, 0)); len(out) != 1 || out[0].ids[0] != r.id {
		t.Fatal("an unrelated player lost the room")
	}
}

func TestCTRHostNobodyReachesGetsNoJoiners(t *testing.T) {
	m := ctrMatchmaker{rooms: map[uint64]*ctrRoom{}}
	r := fillTestRoom(t, &m, 6100, 2)
	m.reach.hostJoined[r.host.pid] = time.Now().Unix() - 60 // 6101 connected a minute ago
	failed := map[uint64]int{6101: 3}
	for _, pid := range []uint64{6200, 6201} {
		m.enqueue(testCTRSearch(t, pid, 0))
		failed[pid] = 3
		hostReports(t, &m, r, failed)
	}
	if !m.reach.unreachable(r.host.pid, time.Now().Unix()) {
		t.Fatal("three different joiners failed: the host should count as unreachable")
	}
	out := m.enqueue(testCTRSearch(t, 6300, 0))
	for _, n := range out {
		if n.ids[0] == r.id {
			t.Fatal("a new player was sent to a host nobody reaches")
		}
	}
	// A joiner that connects after that clears it.
	m.reach.hostJoined[r.host.pid] = time.Now().Unix() + 1
	if m.reach.unreachable(r.host.pid, time.Now().Unix()+1) {
		t.Fatal("a host somebody reached is not unreachable")
	}
}

func TestCTRWaitingSearchNotBackfilledIntoFailedHost(t *testing.T) {
	m := ctrMatchmaker{rooms: map[uint64]*ctrRoom{}}
	r := fillTestRoom(t, &m, 6400, 2)
	m.enqueue(testCTRSearch(t, 6500, 0))
	hostReports(t, &m, r, map[uint64]int{6401: 2, 6500: 3})
	// It searches again while the lobby is closed, so it waits...
	r.hostDoc["lobby_open"] = false
	m.enqueue(testCTRSearch(t, 6500, 0))
	r.hostDoc["lobby_open"] = true
	// ...and the host reopening must not pull it back in.
	hostReports(t, &m, r, map[uint64]int{6401: 2})
	for _, p := range r.players {
		if p.pid == 6500 {
			t.Fatal("waiting search backfilled into the host it failed with")
		}
	}
}

func TestCTRNewLobbyAvoidsUnreachableHost(t *testing.T) {
	m := ctrMatchmaker{rooms: map[uint64]*ctrRoom{}}
	now := time.Now().Unix()
	m.reach = ctrReach{pairFailed: map[[2]uint64]int64{}, hostJoined: map[uint64]int64{},
		hostFailed: map[uint64]map[uint64]int64{6600: {1: now, 2: now, 3: now}}}
	m.enqueue(testCTRSearch(t, 6600, 0))
	out := m.enqueue(testCTRSearch(t, 6601, 0))
	if len(out) != 1 || m.rooms[out[0].ids[1]].host.pid != 6601 {
		t.Fatal("an unreachable player was made host while another one could host")
	}
}
