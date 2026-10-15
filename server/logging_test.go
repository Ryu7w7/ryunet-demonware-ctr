package main

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
)

func captureLog(t *testing.T, debug bool, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	oldDebug := logDebug
	log.SetOutput(&buf)
	logDebug = debug
	t.Cleanup(func() {
		log.SetOutput(os.Stdout)
		logDebug = oldDebug
	})
	fn()
	return buf.String()
}

func TestInfoLevelHidesRoutineTraffic(t *testing.T) {
	l := &lobbyConn{n: 1}
	out := captureLog(t, false, func() {
		l.debugf("routine task")
		debugf("routine packet")
		l.logf("purchase persisted")
	})
	if strings.Contains(out, "routine") || !strings.Contains(out, "purchase persisted") {
		t.Fatalf("info level output:\n%s", out)
	}

	out = captureLog(t, true, func() { l.debugf("routine task") })
	if !strings.Contains(out, "routine task") {
		t.Fatal("debug level hid a debug line")
	}
}

func TestInfoLevelDumpsEachUnhandledTaskOnce(t *testing.T) {
	unhandledSeen.Range(func(k, _ any) bool { unhandledSeen.Delete(k); return true })
	l := &lobbyConn{n: 1}
	out := captureLog(t, false, func() {
		for i := 0; i < 3; i++ {
			l.logUnhandled("task UNHANDLED service=200 task=7", 200, 7, []byte{200, 3, 7})
		}
		l.logUnhandled("task UNHANDLED service=200 task=8", 200, 8, []byte{200, 3, 8})
	})
	if n := strings.Count(out, "task=7"); n != 1 {
		t.Fatalf("task 7 logged %d times, want 1:\n%s", n, out)
	}
	if !strings.Contains(out, "task=8") {
		t.Fatal("a different task was suppressed")
	}
}

func TestLogLevelFromEnv(t *testing.T) {
	old := verbose
	t.Cleanup(func() { verbose = old })
	verbose = false
	t.Setenv("RYUNET_LOG_LEVEL", "")
	t.Setenv("D3_LOG_LEVEL", "INFO")
	if debugLevel() {
		t.Fatal("D3_LOG_LEVEL=INFO kept debug logging")
	}
	t.Setenv("D3_LOG_LEVEL", "")
	if !debugLevel() {
		t.Fatal("default must stay debug")
	}
	verbose = true
	t.Setenv("D3_LOG_LEVEL", "info")
	if !debugLevel() {
		t.Fatal("D3_VERBOSE=1 must imply debug")
	}
}
