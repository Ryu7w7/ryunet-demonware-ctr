package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

var (
	discordAlice = discordUser{ID: "111", Username: "alice"}
	discordBob   = discordUser{ID: "222", Username: "bob", GlobalName: "Bob"}
	keyC         = strings.Repeat("c3", 32)
)

func TestDiscordLinkAndRecover(t *testing.T) {
	r := testNickRegistry(t)
	if s, _ := r.link("Neki", keyA, discordAlice); s != linkLinked {
		t.Fatalf("free nickname: %s, want linked", s)
	}
	if st, linked, name := r.statusInfo("neki", keyA); st != nickYours || !linked || name != "alice" {
		t.Fatalf("owner status: %s linked=%v name=%q", st, linked, name)
	}
	// A reinstalled PC (new key): taken until Discord recovers it, then both installations work.
	if st, linked, name := r.statusInfo("Neki", keyB); st != nickTaken || !linked || name != "" {
		t.Fatalf("other installation sees: %s linked=%v name=%q", st, linked, name)
	}
	if ok, _ := r.admit("Neki", keyB); ok {
		t.Fatal("new installation logged in before recovering")
	}
	if s, _ := r.link("Neki", keyB, discordAlice); s != linkRecovered {
		t.Fatalf("recovery: %s", s)
	}
	for _, k := range []string{keyA, keyB} {
		if ok, why := r.admit("Neki", k); !ok {
			t.Fatalf("installation refused after recovery: %s", why)
		}
	}
	// Someone else's Discord account cannot take it.
	if s, _ := r.link("Neki", keyC, discordBob); s != linkTaken {
		t.Fatalf("other Discord account: %s", s)
	}
	// One nickname per Discord account.
	if s, other := r.link("Neki2", keyA, discordAlice); s != linkOtherNick || other != "Neki" {
		t.Fatalf("second nickname: %s %q", s, other)
	}
}

func TestDiscordLinkUnlinkedNickname(t *testing.T) {
	r := testNickRegistry(t)
	if ok, _ := r.admit("Ryu7w7", keyA); !ok {
		t.Fatal("claim refused")
	}
	if s, _ := r.link("Ryu7w7", keyB, discordBob); s != linkTaken {
		t.Fatalf("another installation linked someone's nickname: %s", s)
	}
	if s, _ := r.link("Ryu7w7", keyA, discordBob); s != linkLinked {
		t.Fatalf("owner could not link: %s", s)
	}
	if r.unlink("Ryu7w7", keyB) {
		t.Fatal("a stranger unlinked it")
	}
	if !r.unlink("Ryu7w7", keyA) {
		t.Fatal("owner could not unlink")
	}
	if _, linked, _ := r.statusInfo("Ryu7w7", keyA); linked {
		t.Fatal("still linked")
	}
	// The Discord account is free for another nickname again.
	if s, _ := r.link("Other", keyC, discordBob); s != linkLinked {
		t.Fatalf("after unlink: %s", s)
	}
}

func TestDiscordLinkSurvivesRestart(t *testing.T) {
	r := testNickRegistry(t)
	if s, _ := r.link("Neki", keyA, discordAlice); s != linkLinked {
		t.Fatal(s)
	}
	saved := nicks
	defer func() { nicks = saved }()
	nicks = newNickRegistry("")
	t.Setenv("RYUNET_NICKS", r.path)
	loadNickClaims()
	if s, _ := nicks.link("Neki", keyB, discordAlice); s != linkRecovered {
		t.Fatalf("after a restart: %s", s)
	}
	if s, other := nicks.link("Else", keyC, discordAlice); s != linkOtherNick || other != "Neki" {
		t.Fatalf("Discord index not rebuilt: %s %q", s, other)
	}
}

func TestUnlinkedNicknamesExpire(t *testing.T) {
	r := testNickRegistry(t)
	t.Setenv("RYUNET_NICK_EXPIRE_DAYS", "60")
	r.admit("Old", keyA)
	r.link("Kept", keyB, discordAlice)
	old := time.Now().Unix() - 61*86400
	for _, n := range []string{"old", "kept"} {
		r.claims[n].Claimed, r.claims[n].LastSeen = old, old
	}
	if st := r.status("Old", keyC); st != nickFree {
		t.Fatalf("unused unlinked nickname: %s, want free", st)
	}
	if ok, _ := r.admit("Old", keyC); !ok {
		t.Fatal("expired nickname not claimable")
	}
	if st := r.status("Kept", keyC); st != nickTaken {
		t.Fatalf("linked nickname expired: %s", st)
	}
	t.Setenv("RYUNET_NICK_EXPIRE_DAYS", "0")
	r.claims["old"].Claimed, r.claims["old"].LastSeen = old, old
	if st := r.status("Old", keyA); st != nickTaken {
		t.Fatalf("expiry off: %s", st)
	}
}

func TestNickRelease(t *testing.T) {
	r := testNickRegistry(t)
	r.link("Neki", keyA, discordAlice)
	if !r.release("NEKI") {
		t.Fatal("release failed")
	}
	if st := r.status("Neki", keyB); st != nickFree {
		t.Fatalf("after release: %s", st)
	}
	if s, _ := r.link("Another", keyB, discordAlice); s != linkLinked {
		t.Fatalf("Discord account kept after release: %s", s)
	}
	if r.release("nobody") {
		t.Fatal("released a nickname that was never claimed")
	}
}

// The HTTP side with a fake Discord: code exchange, redirect check, replies.
func TestDiscordLinkEndpoint(t *testing.T) {
	saved := nicks
	defer func() { nicks = saved }()
	nicks = testNickRegistry(t)
	t.Setenv("DISCORD_CLIENT_SECRET", "secret")
	t.Setenv("RYUNET_DISCORD_REDIRECTS", "")
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			_ = r.ParseForm()
			if r.PostForm.Get("client_secret") != "secret" || r.PostForm.Get("code") != "good" ||
				r.PostForm.Get("redirect_uri") != "http://127.0.0.1:53134/discord" {
				http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer"}`))
		case "/users/@me":
			if r.Header.Get("Authorization") != "Bearer tok" {
				http.Error(w, "no", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"id":"111","username":"alice","global_name":"Alice"}`))
		}
	}))
	defer fake.Close()
	old := discordAPI
	discordAPI = fake.URL
	defer func() { discordAPI = old }()

	post := func(code, redirect string) (int, map[string]string) {
		body := url.Values{"nick": {"Neki"}, "code": {code}, "redirect_uri": {redirect}}.Encode()
		req := httptest.NewRequest(http.MethodPost, "/ryunet/discord/link", strings.NewReader(body))
		req.Header.Set(nickKeyHeader, keyA)
		rec := httptest.NewRecorder()
		handleDiscordLink(rec, req, []byte(body))
		var reply map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &reply)
		return rec.Code, reply
	}
	if code, reply := post("good", "http://evil.example/discord"); code != http.StatusBadRequest {
		t.Fatalf("unregistered redirect: %d %v", code, reply)
	}
	if code, reply := post("bad", "http://127.0.0.1:53134/discord"); code != http.StatusBadGateway || reply["status"] != linkError {
		t.Fatalf("rejected code: %d %v", code, reply)
	}
	code, reply := post("good", "http://127.0.0.1:53134/discord")
	if code != http.StatusOK || reply["status"] != linkLinked || reply["discord"] != "Alice" {
		t.Fatalf("link: %d %v", code, reply)
	}
	// The nickname check now tells its owner the linked account.
	req := httptest.NewRequest(http.MethodGet, "/ryunet/nick?name=Neki", nil)
	req.Header.Set(nickKeyHeader, keyA)
	rec := httptest.NewRecorder()
	handleNickCheck(rec, req)
	var check map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &check)
	if check["status"] != nickYours || check["linked"] != true || check["discord"] != "Alice" || check["discord_link"] != true {
		t.Fatalf("nick check: %v", check)
	}
	// Without a secret the server says linking is off.
	t.Setenv("DISCORD_CLIENT_SECRET", "")
	if code, reply := post("good", "http://127.0.0.1:53134/discord"); code != http.StatusNotImplemented {
		t.Fatalf("unconfigured: %d %v", code, reply)
	}
}

// A reinstalled launcher that forgot the nickname: the Discord account alone recovers it, with
// its own spelling, and the installation may use it at once.
func TestDiscordRecoverForgottenNickname(t *testing.T) {
	r := testNickRegistry(t)
	if s, _ := r.link("Neki", keyA, discordAlice); s != linkLinked {
		t.Fatal(s)
	}
	if s, nick := r.link("", keyB, discordAlice); s != linkRecovered || nick != "Neki" {
		t.Fatalf("recovery without a nickname: %s %q", s, nick)
	}
	if ok, why := r.admit("Neki", keyB); !ok {
		t.Fatalf("recovered installation refused: %s", why)
	}
	if s, _ := r.link("", keyC, discordBob); s != linkNoNick {
		t.Fatalf("account without nickname: %s", s)
	}
	// Trying a new nickname with an account that has one also hands the old one over.
	if s, nick := r.link("Fresh", keyC, discordAlice); s != linkOtherNick || nick != "Neki" {
		t.Fatalf("other nickname: %s %q", s, nick)
	}
	if ok, why := r.admit("Neki", keyC); !ok {
		t.Fatalf("other_nick did not let this installation use it: %s", why)
	}
	if st := r.status("Fresh", keyC); st != nickFree {
		t.Fatalf("the refused new nickname was claimed: %s", st)
	}
}

// Recovery with nothing linked to the account: the nickname this installation already owns.
func TestDiscordRecoverInstallationNickname(t *testing.T) {
	r := testNickRegistry(t)
	r.link("Ryu7w7", keyA, discordAlice)
	r.unlink("Ryu7w7", keyA)
	if s, nick := r.link("", keyA, discordAlice); s != linkOwnNick || nick != "Ryu7w7" {
		t.Fatalf("installation's own nickname: %s %q", s, nick)
	}
	if s, _ := r.link("", keyB, discordAlice); s != linkNoNick {
		t.Fatalf("another installation: %s", s)
	}
}
