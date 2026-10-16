package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// inventoryRequest builds a getInventoryPaginated (task 165) the way the PS4 client sends it.
func inventoryRequest(page, size uint32) []byte {
	w := &bdWriter{b: []byte{svcMarketplace, tagU8, taskGetInventoryPaginated}}
	w.str("octane_psn")
	w.u32(page)
	w.u32(size)
	return w.b
}

// Task reply: 0A u64 transaction | 08 u32 error | 03 u8 task | 08 u32 numResults | ...
func replyErrorAndCount(t *testing.T, reply []byte) (uint32, uint32) {
	t.Helper()
	if len(reply) < 21 || reply[0] != tagU64 || reply[9] != tagU32 || reply[16] != tagU32 {
		t.Fatalf("not a task reply: % x", reply[:min(len(reply), 24)])
	}
	return binary.LittleEndian.Uint32(reply[10:14]), binary.LittleEndian.Uint32(reply[17:21])
}

func TestInventoryModeAllOwnsTheWholeCatalog(t *testing.T) {
	t.Setenv("CTR_INVENTORY", "all")
	all := allCatalogItems()
	if len(all) < 700 {
		t.Fatalf("catalog has %d items, expected the whole Pit Stop (~800)", len(all))
	}
	l := &lobbyConn{player: &playerID{PID: 42}}
	errCode, n := replyErrorAndCount(t, l.onMarketplace(taskGetInventoryPaginated, inventoryRequest(1, 4096)))
	if errCode != 0 || int(n) != len(all) {
		t.Fatalf("all mode: error %d, %d items, want 0 and %d", errCode, n, len(all))
	}
	_, n = replyErrorAndCount(t, l.onMarketplace(taskGetInventoryPaginated, inventoryRequest(2, 500)))
	if int(n) != len(all)-500 {
		t.Fatalf("all mode page 2: %d items, want %d", n, len(all)-500)
	}
}

// save (or its alias local): the request fails as it did before octane_psn was accepted, so
// the game keeps the items of its own save.
func TestInventoryModeLocalRefusesTheRequest(t *testing.T) {
	t.Setenv("CTR_INVENTORY", "local")
	l := &lobbyConn{player: &playerID{PID: 42}}
	// An error reply stops after the error code: 0A u64 transaction | 08 u32 error.
	reply := l.onMarketplace(taskGetInventoryPaginated, inventoryRequest(1, 100))
	if len(reply) < 14 || reply[9] != tagU32 || binary.LittleEndian.Uint32(reply[10:14]) == 0 {
		t.Fatalf("local mode answered the inventory: % x", reply)
	}
}

// A player who chose "save" in the launcher gets the pre-fix behavior: no server inventory
// and no purchases, so their save decides and no coin is spent on an item they would not see.
func TestInventoryPlayerChoosesSave(t *testing.T) {
	t.Setenv("CTR_INVENTORY", "")
	saver := &lobbyConn{player: &playerID{PID: 42, SaveInventory: true}}
	if got := saver.inventoryModeFor(); got != "save" {
		t.Fatalf("player choice ignored: %q", got)
	}
	if r := saver.onMarketplace(taskGetInventoryPaginated, inventoryRequest(1, 100)); binary.LittleEndian.Uint32(r[10:14]) == 0 {
		t.Fatal("inventory answered to a player keeping their save's items")
	}
	purchase, _ := hex.DecodeString(realPurchase)
	purchase = bytes.Replace(purchase, []byte("octane_switch"), []byte("octane_psn"), 1)
	if r := saver.onMarketplace(taskPurchaseSkus, purchase); binary.LittleEndian.Uint32(r[10:14]) == 0 {
		t.Fatal("purchase accepted from a player keeping their save's items")
	}

	buyer := &lobbyConn{player: &playerID{PID: 43}}
	if got := buyer.inventoryModeFor(); got != "server" {
		t.Fatalf("default player mode %q, want server", got)
	}
	// The server-wide setting wins over the player's choice.
	t.Setenv("CTR_INVENTORY", "all")
	if got := saver.inventoryModeFor(); got != "all" {
		t.Fatalf("CTR_INVENTORY=all overridden by the player: %q", got)
	}
}

// The launcher's choice travels as a header on the login request and must reach the identity
// the lobby recovers from the ticket.
func TestInventoryChoiceTravelsWithTheLogin(t *testing.T) {
	t.Setenv("RYUNET_NICK_CLAIMS", "0")
	login := func(nick, choice string) *playerID {
		t.Helper()
		body := `{"title_id":"5760","auth_task":"47","extra_data":"{\"username\":\"` + nick + `\"}"}`
		req := httptest.NewRequest(http.MethodPost, "/auth/", strings.NewReader(body))
		if choice != "" {
			req.Header.Set(inventoryHeader, choice)
		}
		w := httptest.NewRecorder()
		authHandler("", nil)(w, req)
		var resp struct {
			ClientTicket string `json:"client_ticket"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("login %s: %v (%s)", nick, err, w.Body.String())
		}
		ticket, _ := base64.StdEncoding.DecodeString(resp.ClientTicket)
		id, ok := issuedIdentity(ticket)
		if !ok {
			t.Fatalf("login %s: no identity for the issued ticket", nick)
		}
		return id
	}
	if id := login("SaveFan01", "save"); !id.SaveInventory {
		t.Fatal("\"save\" chosen in the launcher did not reach the lobby identity")
	}
	if id := login("Grinder01", ""); id.SaveInventory {
		t.Fatal("a player who chose nothing keeps their save's items")
	}
}

func TestInventoryModeDefaultsToServer(t *testing.T) {
	for _, v := range []string{"", "server", "nonsense"} {
		t.Setenv("CTR_INVENTORY", v)
		if got := inventoryMode(); got != "server" {
			t.Fatalf("CTR_INVENTORY=%q -> %q, want server", v, got)
		}
	}
	t.Setenv("CTR_INVENTORY", " ALL ")
	if inventoryMode() != "all" {
		t.Fatal("mode is not case- and space-insensitive")
	}
}
