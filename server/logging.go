package main

import (
	"encoding/hex"
	"log"
	"strings"
	"sync"
)

// Log levels. "debug" (default) logs every task, packet and request, which is what
// local reverse engineering needs. "info" is meant for a busy production server: it
// keeps startup, errors, refusals, purchases and players joining/leaving, and drops
// the per-task chatter that otherwise scrolls by on every lobby message.
//
//	RYUNET_LOG_LEVEL / D3_LOG_LEVEL = debug | info
var logDebug = debugLevel()

func debugLevel() bool {
	if verbose {
		return true
	}
	lvl := strings.ToLower(envOr("RYUNET_LOG_LEVEL", envOr("D3_LOG_LEVEL", "debug")))
	return lvl != "info"
}

// debugf logs routine traffic, only at the debug level.
func debugf(format string, a ...any) {
	if logDebug {
		log.Printf(format, a...)
	}
}

func (l *lobbyConn) debugf(format string, a ...any) {
	if logDebug {
		l.logf(format, a...)
	}
}

var unhandledSeen sync.Map

// logUnhandled reports a task the server does not implement. At the info level each
// service/task pair is dumped once per process: the game repeats the same ones on
// every login, and the hex dump is by far the largest log line.
func (l *lobbyConn) logUnhandled(what string, service, task byte, payload []byte) {
	if !logDebug {
		if _, seen := unhandledSeen.LoadOrStore(uint16(service)<<8|uint16(task), true); seen {
			return
		}
		what += " (first occurrence, repeats hidden at log level info)"
	}
	l.logf("%s:\n%s", what, hex.Dump(payload))
}
