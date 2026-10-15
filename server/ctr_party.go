package main

import (
	"os"
	"sync"
)

// Party members in async matchmaking.
//
// Only the party leader sends the search; it lists every member. Production showed
// the leader entering the lobby and its members being timed out by the host (state 3)
// in 83% of placed parties, and the members never ask for the lobby documents
// themselves. The game's event handler (0xdaf30, events 2000..2008) hands the search
// ID of each join/merge event to the game, and the party replicates the leader's
// _asyncMatchMakingID: each member expects the same event the leader gets. Sending it
// to them was confirmed in game (2026-10-15): the whole party now enters the lobby.
// CTR_PARTY_NOTIFY_MEMBERS=0 turns it off.
func partyNotifyEnabled() bool { return os.Getenv("CTR_PARTY_NOTIFY_MEMBERS") != "0" }

var (
	connByPIDMu sync.Mutex
	connByPID   = map[uint64]*lobbyConn{}
)

// rememberConn records the latest lobby connection of an identified player.
func rememberConn(l *lobbyConn) {
	if pid := l.pid(); pid != 0 {
		connByPIDMu.Lock()
		connByPID[pid] = l
		connByPIDMu.Unlock()
	}
}

// forgetConn drops the connection, unless the player already reconnected elsewhere.
func forgetConn(l *lobbyConn) {
	pid := l.pid()
	if pid == 0 {
		return
	}
	connByPIDMu.Lock()
	if connByPID[pid] == l {
		delete(connByPID, pid)
	}
	connByPIDMu.Unlock()
}

func connForPID(pid uint64) *lobbyConn {
	connByPIDMu.Lock()
	defer connByPIDMu.Unlock()
	return connByPID[pid]
}

// memberNotices returns event notices for the members of search p other than the
// leader who searched, each carrying the leader's search ID. Members already told
// about this room are skipped. Holds the matchmaker lock (called from syncDoc and
// finishMerge).
func (r *ctrRoom) memberNotices(p *ctrSearch, event uint32, ids []uint64) []ctrNotice {
	if !partyNotifyEnabled() || len(p.doc.Members) < 2 {
		return nil
	}
	if r.notifiedPID == nil {
		r.notifiedPID = map[uint64]bool{}
	}
	var out []ctrNotice
	var host, backend string
	for _, pid := range p.doc.Members {
		if pid == p.pid || r.notifiedPID[pid] || r.failed(pid) {
			continue
		}
		conn := connForPID(pid)
		if conn == nil || conn == p.conn {
			continue
		}
		if host == "" {
			host, backend = r.documents()
		}
		r.notifiedPID[pid] = true
		conn.logf("CTR party member pid=%d told event=%d room=%d leader=%d search=%d", pid, event, r.id, p.pid, p.id)
		out = append(out, ctrNotice{conn, event, ids, host, backend})
	}
	return out
}
