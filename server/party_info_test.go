package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func partyInfo(t *testing.T, name string) (uint32, uint32) {
	t.Helper()
	w := httptest.NewRecorder()
	handlePartyInfo(w, httptest.NewRequest(http.MethodGet, "/ryunet/party?name="+url.QueryEscape(name), nil))
	var got struct{ Players, Max uint32 }
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got.Players, got.Max
}

// The launcher sizes the Discord party from the lobby the player hosts.
func TestPartyInfoReportsTheHostedLobby(t *testing.T) {
	const nick = "TDYU"
	host := &lobbyConn{n: 9301, player: &playerID{Username: nick, PID: calcPS4ID(nick, true)}}
	t.Cleanup(func() {
		sessionsMu.Lock()
		for id, s := range sessions {
			if s.ownerPID == host.player.PID {
				delete(sessions, id)
			}
		}
		sessionsMu.Unlock()
	})

	info := &bdWriter{}
	info.blobv(make([]byte, 67))
	info.u32(16)
	info.u32(8)
	reply := host.onMatchMakingContext(mmCreateSession, &bdReader{b: info.b}, "ctr:Delta")
	r := &bdReader{b: reply, off: 1 + 8 + 1 + 4 + 2 + 1 + 4 + 1 + 4}
	sid, err := r.blob()
	if err != nil || len(sid) != 8 {
		t.Fatalf("session id: %v % x", err, reply)
	}
	if p, m := partyInfo(t, "tdyu"); p != 1 || m != 8 {
		t.Fatalf("new lobby = %d/%d, want 1/8", p, m)
	}

	upd := &bdWriter{}
	upd.blobv(sid)
	upd.u32(3)
	upd.raw(info.b)
	host.onMatchMakingContext(mmUpdatePlayers, &bdReader{b: upd.b}, "ctr:Delta")
	if p, m := partyInfo(t, nick); p != 3 || m != 8 {
		t.Fatalf("after 3 joined = %d/%d, want 3/8", p, m)
	}

	if p, _ := partyInfo(t, "NobodyHosts"); p != 0 {
		t.Fatal("a player hosting nothing reported a lobby")
	}
	if p, _ := partyInfo(t, "bad name"); p != 0 {
		t.Fatal("an invalid nickname reported a lobby")
	}

	// The host disconnected: its lobby is orphaned and cannot be joined.
	dropSessionsOf(host.n)
	if p, _ := partyInfo(t, nick); p != 0 {
		t.Fatal("an orphaned lobby was reported as joinable")
	}
}
