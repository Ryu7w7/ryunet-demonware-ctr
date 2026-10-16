package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func testNickRegistry(t *testing.T) *nickRegistry {
	t.Helper()
	t.Setenv("RYUNET_NICK_CLAIMS", "")
	t.Setenv("RYUNET_NICK_REQUIRE_KEY", "")
	return newNickRegistry(filepath.Join(t.TempDir(), "nick-claims.json"))
}

var (
	keyA = strings.Repeat("a1", 32)
	keyB = strings.Repeat("b2", 32)
)

func TestNickFirstKeyOwnsIt(t *testing.T) {
	r := testNickRegistry(t)
	if ok, why := r.admit("Ryu7w7", keyA); !ok {
		t.Fatalf("first claim refused: %s", why)
	}
	if ok, _ := r.admit("ryu7W7", keyA); !ok {
		t.Fatal("owner refused (nicknames are case-insensitive)")
	}
	if ok, _ := r.admit("Ryu7w7", keyB); ok {
		t.Fatal("another installation took a claimed nickname")
	}
	if ok, _ := r.admit("Ryu7w7", ""); ok {
		t.Fatal("a client without key took a claimed nickname")
	}
}

func TestNickReservedAndMalformed(t *testing.T) {
	r := testNickRegistry(t)
	for _, n := range []string{"RyuPC", "ryupc", "Player", "RyuNET"} {
		if ok, _ := r.admit(n, keyA); ok {
			t.Fatalf("reserved nickname %q admitted", n)
		}
		if ok, _ := r.admit(n, ""); ok {
			t.Fatalf("reserved nickname %q admitted without key", n)
		}
	}
	if ok, _ := r.admit("Someone", "not-hex"); ok {
		t.Fatal("malformed key admitted")
	}
	if ok, _ := r.admit("bad name!", keyA); ok {
		t.Fatal("invalid nickname claimed")
	}
	if len(r.claims) != 0 {
		t.Fatalf("refusals left claims: %v", r.claims)
	}
}

// A client from before keys keeps working on a nickname nobody claimed, so an update is
// not forced on everyone at once; it never claims.
func TestNickLegacyClientOnFreeName(t *testing.T) {
	r := testNickRegistry(t)
	if ok, _ := r.admit("OldClient", ""); !ok {
		t.Fatal("legacy client refused on a free nickname")
	}
	if len(r.claims) != 0 {
		t.Fatal("a keyless login claimed the nickname")
	}
	if ok, _ := r.admit("OldClient", keyB); !ok {
		t.Fatal("the nickname could not be claimed after a keyless login")
	}
}

func TestNickClaimsSurviveRestart(t *testing.T) {
	r := testNickRegistry(t)
	r.admit("ESDRONIX", keyA)
	t.Setenv("RYUNET_NICKS", r.path)
	saved := nicks
	t.Cleanup(func() { nicks = saved })
	nicks = newNickRegistry("")
	loadNickClaims()
	if ok, _ := nicks.admit("ESDRONIX", keyB); ok {
		t.Fatal("claim lost across a restart")
	}
	if ok, _ := nicks.admit("ESDRONIX", keyA); !ok {
		t.Fatal("owner refused after a restart")
	}
}

// Once keys are required, an old launcher cannot share an unclaimed nickname any more.
func TestNickKeyRequired(t *testing.T) {
	r := testNickRegistry(t)
	t.Setenv("RYUNET_NICK_REQUIRE_KEY", "1")
	if ok, _ := r.admit("OldClient", ""); ok {
		t.Fatal("a client without key admitted while keys are required")
	}
	if ok, _ := r.admit("NewClient", keyA); !ok {
		t.Fatal("a client with a key refused while keys are required")
	}
}

// col4048 and col6947 hash to the same PID (found by brute force): the second must not get
// the first one's wallet and identity.
func TestNickPIDCollisionRefused(t *testing.T) {
	if nickPID("col4048") != nickPID("col6947") {
		t.Fatal("fixture no longer collides")
	}
	r := testNickRegistry(t)
	if ok, _ := r.admit("col4048", keyA); !ok {
		t.Fatal("first nickname refused")
	}
	if ok, why := r.admit("col6947", keyB); ok {
		t.Fatal("a nickname sharing a claimed PID was claimed")
	} else if !strings.Contains(why, "col4048") {
		t.Fatalf("reason does not name the clash: %s", why)
	}
	if ok, _ := r.admit("col6947", ""); ok {
		t.Fatal("a keyless client took a PID that belongs to a claimed nickname")
	}
	if got := r.status("COL6947", keyB); got != nickTaken {
		t.Fatalf("check says %q for a colliding nickname, want taken", got)
	}
	if ok, _ := r.admit("col4048", keyA); !ok {
		t.Fatal("the owner of the PID was refused")
	}

	// The index is rebuilt from the file after a restart.
	t.Setenv("RYUNET_NICKS", r.path)
	saved := nicks
	t.Cleanup(func() { nicks = saved })
	nicks = newNickRegistry("")
	loadNickClaims()
	if ok, _ := nicks.admit("col6947", keyB); ok {
		t.Fatal("collision check lost across a restart")
	}
}

func TestNickClaimsCanBeTurnedOff(t *testing.T) {
	r := testNickRegistry(t)
	r.admit("Ryu7w7", keyA)
	t.Setenv("RYUNET_NICK_CLAIMS", "0")
	if ok, _ := r.admit("Ryu7w7", keyB); !ok {
		t.Fatal("RYUNET_NICK_CLAIMS=0 still enforced claims")
	}
}

func TestNickCheckEndpoint(t *testing.T) {
	r := testNickRegistry(t)
	r.admit("Ryu7w7", keyA)
	saved := nicks
	t.Cleanup(func() { nicks = saved })
	nicks = r
	for _, c := range []struct{ name, key, want string }{
		{"Ryu7w7", keyA, nickYours},
		{"ryu7w7", keyB, nickTaken},
		{"Ryu7w7", "", nickTaken},
		{"Nuevo_1", keyB, nickFree},
		{"RyuPC", keyB, nickReserved},
		{"a b", keyB, nickInvalid},
	} {
		req := httptest.NewRequest(http.MethodGet, "/ryunet/nick?name="+strings.ReplaceAll(c.name, " ", "%20"), nil)
		if c.key != "" {
			req.Header.Set(nickKeyHeader, c.key)
		}
		w := httptest.NewRecorder()
		handleNickCheck(w, req)
		var got struct{ Status string }
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.Status != c.want {
			t.Fatalf("%q key=%.4s: status %q (%v), want %q", c.name, c.key, got.Status, err, c.want)
		}
	}
	if len(r.claims) != 1 {
		t.Fatal("the check endpoint claimed a nickname")
	}
}
