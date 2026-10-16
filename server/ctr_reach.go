package main

import (
	"strconv"
)

// Which hosts players can actually reach.
//
// A joiner that cannot reach its host is reported by the host as state 3, its seat is released
// (syncDoc) and its game searches again at once. Before this the server put it straight back
// into the same lobby (the waiting-search backfill, and enqueue, both clear its failed state),
// so it failed against the same host over and over: on production (2026-10-16) single rooms
// released "failed reservations" 20 to 28 times, a few hosts that nobody could reach (strict or
// carrier NAT) took most of the 205 failures in half an hour, and a party placed with one of
// them never got in, while the same party matched fine on a LAN server where it hosted itself.
//
// Two rules, for ctrReachWindow after a failure:
//   - a joiner (or any member of its party) is not sent to a host it failed with;
//   - a host that ctrUnreachableAfter different joiners failed with, and nobody reached since,
//     gets no joiners at all, and is not picked to host a new lobby while its group has
//     another candidate.
//
// Both rules only skip rooms; a search that skips them all waits or builds a lobby of its own
// as before.

const (
	ctrReachWindow      = 15 * 60 // seconds
	ctrUnreachableAfter = 3
)

type ctrReach struct {
	pairFailed map[[2]uint64]int64         // {host, joiner} -> when the joiner failed with the host
	hostFailed map[uint64]map[uint64]int64 // host -> joiner -> when it failed
	hostJoined map[uint64]int64            // host -> when a joiner last connected (state 2)
}

// noteOutcomes reads the host's player_state: state 3 is a joiner that failed to reach it,
// state 2 one that connected. Holds m.mu.
func (m *ctrMatchmaker) noteOutcomes(r *ctrRoom, now int64) {
	states, _ := r.hostDoc["player_state"].(map[string]any)
	if len(states) == 0 || r.host == nil {
		return
	}
	reach := &m.reach
	if reach.pairFailed == nil {
		reach.pairFailed = map[[2]uint64]int64{}
		reach.hostFailed = map[uint64]map[uint64]int64{}
		reach.hostJoined = map[uint64]int64{}
	}
	host := r.host.pid
	for key := range states {
		pid, err := strconv.ParseUint(key, 10, 64)
		if err != nil || pid == host {
			continue
		}
		switch r.state(pid) {
		case 3:
			if _, seen := reach.pairFailed[[2]uint64{host, pid}]; !seen {
				r.host.conn.logf("CTR join failed: pid=%d could not reach host pid=%d (room=%d); not matched with it again for %d min",
					pid, host, r.id, ctrReachWindow/60)
			}
			reach.pairFailed[[2]uint64{host, pid}] = now
			if reach.hostFailed[host] == nil {
				reach.hostFailed[host] = map[uint64]int64{}
			}
			reach.hostFailed[host][pid] = now
		case 2:
			reach.hostJoined[host] = now
		}
	}
	reach.prune(now)
}

// prune forgets outcomes older than the window. Holds m.mu.
func (reach *ctrReach) prune(now int64) {
	for key, at := range reach.pairFailed {
		if now-at > ctrReachWindow {
			delete(reach.pairFailed, key)
		}
	}
	for host, joiners := range reach.hostFailed {
		for pid, at := range joiners {
			if now-at > ctrReachWindow {
				delete(joiners, pid)
			}
		}
		if len(joiners) == 0 {
			delete(reach.hostFailed, host)
		}
	}
	for host, at := range reach.hostJoined {
		if now-at > ctrReachWindow {
			delete(reach.hostJoined, host)
		}
	}
}

// unreachable: enough different joiners failed with this host and none connected after the
// earliest of those failures. Holds m.mu.
func (reach *ctrReach) unreachable(host uint64, now int64) bool {
	joiners := reach.hostFailed[host]
	failures, first := 0, now
	for _, at := range joiners {
		if now-at <= ctrReachWindow {
			failures++
			first = min(first, at)
		}
	}
	return failures >= ctrUnreachableAfter && reach.hostJoined[host] < first
}

// avoid is why search s must not be sent to this host's lobby ("" when it may). Holds m.mu.
func (reach *ctrReach) avoid(host uint64, s *ctrSearch, now int64) string {
	for _, pid := range s.doc.Members {
		if at, ok := reach.pairFailed[[2]uint64{host, pid}]; ok && now-at <= ctrReachWindow {
			return "failed-with-host"
		}
	}
	if reach.unreachable(host, now) {
		return "host-unreachable"
	}
	return ""
}

// pickHost: the search to host a new lobby, the first one whose leader is not known to be
// unreachable (the first one when they all are). Holds m.mu.
func (reach *ctrReach) pickHost(group []*ctrSearch, now int64) *ctrSearch {
	for _, p := range group {
		if !reach.unreachable(p.pid, now) {
			return p
		}
	}
	return group[0]
}
