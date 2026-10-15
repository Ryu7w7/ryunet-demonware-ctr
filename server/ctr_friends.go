package main

// CTR Demonware friends API (friends.dev.demonware.net, plain HTTP).
//
// URL templates found in the 1.21 eboot rodata (~0x1bef504):
//
//	%s/v1/friends/%s-%llu/?context=%s&client=%s
//	%s/v1/friends/%s-%llu/invitation/?context=%s&client=%s
//	%s/v1/friends/%s-%llu/pending/?context=%s&direction=%s&client=%s
//	%s/v1/friends/%s-%llu/%s-%llu/?context=%s&client=%s
//
// The base is always http://friends.dev.demonware.net (no cert/prod
// variants exist in the image), redirected to us via host_overrides.json.
//
// Round 1: route, log everything (method/URL/body) and dump bodies so the
// first live run shows the exact methods, query shapes and payloads the
// game uses. Answers are minimal valid JSON ({}) which the client
// tolerates as "no friends" (same posture as UNHANDLED lobby tasks).
// Round 2 (after observing): return the real online friends (PIDs the
// game can then resolve through 138/14 friend sessions).
//
// NOTE: this must stay BEFORE the catch-all auth processing in
// authHandler: without routing, a friends request would be answered with
// an auth ticket JSON, which is actively harmful.

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func handleCTRFriends(w http.ResponseWriter, r *http.Request, body []byte, n uint64, dumps string) {
	q := r.URL.Query()
	debugf("[CTR Friends] #%d %s %s%s from %s (%d bytes body) context=%q client=%q",
		n, r.Method, r.Host, r.URL.RequestURI(), r.RemoteAddr, len(body),
		q.Get("context"), q.Get("client"))
	if dumps != "" && len(body) > 0 {
		name := fmt.Sprintf("%03d_%s_friends_%s.bin", n, r.Method,
			strings.NewReplacer("/", "_", "?", "_", "&", "_", ":", "_").Replace(strings.TrimPrefix(r.URL.RequestURI(), "/")))
		if err := os.WriteFile(filepath.Join(dumps, name), body, 0o644); err != nil {
			log.Printf("[CTR Friends] dump: %v", err)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{}`))
}
