package main

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// Two-player private-room flow with the real nick-derived PIDs.
//
// Ryu7w7=1818042688 hosts, ESDRONIX=1831940823 asks "who hosts?".
// Covers the whole friend path the game needs for private rooms:
// 138/14 friend sessions, 68 presence SET+GET, 12/9 getUserNames.
func TestCTRFriendsEndToEndPrivateRoom(t *testing.T) {
	const ryuPID = 1818042688
	const esdPID = 1831940823

	ryu := &lobbyConn{n: 7101, player: &playerID{Username: "Ryu7w7", PID: ryuPID}}
	esd := &lobbyConn{n: 7102, player: &playerID{Username: "ESDRONIX", PID: esdPID}}
	onlineMu.Lock()
	online[ryu.n] = ryu.player
	online[esd.n] = esd.player
	onlineMu.Unlock()
	defer func() {
		onlineMu.Lock()
		delete(online, ryu.n)
		delete(online, esd.n)
		onlineMu.Unlock()
		sessionsMu.Lock()
		for id, s := range sessions {
			if s.ownerPID == ryuPID || s.ownerPID == esdPID {
				delete(sessions, id)
			}
		}
		sessionsMu.Unlock()
		richMu.Lock()
		delete(rich, ryuPID)
		delete(rich, esdPID)
		richMu.Unlock()
	}()

	// 0. Identity sanity: the nick-derived PID formula both sides share.
	if got := calcPS4ID("Ryu7w7", true); got != ryuPID {
		t.Fatalf("Ryu7w7 PID mismatch: got %d want %d", got, ryuPID)
	}
	if got := calcPS4ID("ESDRONIX", true); got != esdPID {
		t.Fatalf("ESDRONIX PID mismatch: got %d want %d", got, esdPID)
	}

	// 1. Host creates a session in the CTR context (as both games do at login).
	info := &bdWriter{}
	info.blobv(make([]byte, 67))
	info.u32(8)
	info.u32(8)
	ryu.onMatchMakingContext(mmCreateSession, &bdReader{b: info.b}, "ctr:octane_psn")

	// 2. Guest asks "of these ids, who hosts a game?" for the host PID.
	ask := &bdWriter{}
	ask.u64(ryuPID)
	reply := esd.onFriendSessions(14, &bdReader{b: ask.b}, "ctr:octane_psn")
	if n := binary.LittleEndian.Uint32(reply[17:21]); n != 1 {
		t.Fatalf("friend session not offered: got %d rooms, want 1", n)
	}

	// 3. Host sets rich presence; guest reads it back.
	setp := &bdWriter{}
	setp.str("octane_psn")
	setp.u64(0) // 0 = me
	setp.str("ps4")
	setp.u8(1)
	setp.blobv([]byte("In lobby"))
	ryu.onRichPresence(rpSet, &bdReader{b: setp.b})
	getp := &bdWriter{}
	getp.str("octane_psn")
	getp.u32(1)
	getp.u64(ryuPID)
	getp.str("psn")
	greply := esd.onRichPresence(rpGet, &bdReader{b: getp.b})
	if !bytes.Contains(greply, []byte("In lobby")) {
		t.Fatalf("guest did not get host presence back")
	}

	// 4. getUserNames resolves the friend PID to the nick.
	ask2 := &bdWriter{}
	ask2.u64(esdPID)
	nreply := ryu.onGetUserNames(9, &bdReader{b: ask2.b})
	if !bytes.Contains(nreply, []byte("ESDRONIX")) {
		t.Fatalf("friend PID did not resolve to nick")
	}

	// 5. 12/10 resolveNicks: nick in -> PID out (what the game sends at login).
 ask3 := &bdWriter{}
	ask3.u32(1)
	ask3.str("ESDRONIX")
	rreply := ryu.onResolveNicks(10, &bdReader{b: ask3.b})
	if !bytes.Contains(rreply, []byte("ESDRONIX")) {
		t.Fatalf("resolveNicks did not return the nick")
	}
	r := &bdReader{b: rreply}
	var sawPID bool
	for r.off < len(r.b) {
		if r.b[r.off] == tagU64 && r.off+9 <= len(r.b) {
			if binary.LittleEndian.Uint64(r.b[r.off+1:]) == esdPID {
				sawPID = true
			}
			r.off += 9
		} else {
			r.off++
		}
	}
	if !sawPID {
		t.Fatalf("resolveNicks reply lacks PID %d", esdPID)
	}

	// 6. Unknown nick resolves to nothing (omitted, like 12/9).
	ask4 := &bdWriter{}
	ask4.u32(1)
	ask4.str("NobodyHere")
	ureply := ryu.onResolveNicks(10, &bdReader{b: ask4.b})
	if bytes.Contains(ureply, []byte("NobodyHere")) {
		t.Fatalf("unknown nick should resolve to nothing")
	}
}
