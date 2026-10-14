package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// The friends API must be routed (not answered as auth) and reply valid JSON.
func TestCTRFriendsRouted(t *testing.T) {
	h := authHandler(t.TempDir(), nil)
	call := func(method, target string, body string) *httptest.ResponseRecorder {
		var rdr *strings.Reader
		if body == "" {
			rdr = strings.NewReader("")
		} else {
			rdr = strings.NewReader(body)
		}
		r := httptest.NewRequest(method, target, rdr)
		r.Host = "friends.dev.demonware.net"
		w := httptest.NewRecorder()
		h(w, r)
		return w
	}

	paths := []string{
		"/v1/friends/ctr-1831940823/?context=octane_psn&client=beenox-crash-ctr-ps4",
		"/v1/friends/ctr-1831940823/invitation/?context=octane_psn&client=beenox-crash-ctr-ps4",
		"/v1/friends/ctr-1831940823/pending/?context=octane_psn&direction=incoming&client=beenox-crash-ctr-ps4",
		"/v1/friends/ctr-1831940823/ctr-1818042688/?context=octane_psn&client=beenox-crash-ctr-ps4",
	}
	for _, p := range paths {
		w := call("GET", p, "")
		if w.Code != 200 {
			t.Fatalf("GET %s -> %d, want 200", p, w.Code)
		}
		var v any
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatalf("GET %s reply is not JSON: %v (%q)", p, err, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "auth_task") {
			t.Fatalf("GET %s answered as auth (misrouted)", p)
		}
	}

	w := call("POST", "/v1/friends/ctr-1831940823/invitation/?context=octane_psn&client=beenox-crash-ctr-ps4", `{"to":1831940823}`)
	if w.Code != 200 {
		t.Fatalf("POST invitation -> %d, want 200", w.Code)
	}
}
