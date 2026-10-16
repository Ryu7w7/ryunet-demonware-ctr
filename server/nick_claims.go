package main

// Nickname ownership for PS4/PC players.
//
// The PID is derived from the nickname alone (calcPS4ID), so before this anyone typing a
// nickname WAS that player: same wallet, same friends, and two players on one name evicting
// each other from matchmaking. Each installation now holds a random secret (the launcher or
// run.py writes it, the port sends it as the X-RyuNET-Key header with every request to this
// server), and the first key to use a nickname owns it:
//
//	reserved nickname (the old shared default, RyuPC...)  refused
//	unclaimed, with a key                                 claimed for that key
//	claimed, same key                                     allowed
//	claimed, another key or no key                        refused
//	unclaimed, no key (a client from before this)         allowed, so updating is not forced,
//	                                                      until RYUNET_NICK_REQUIRE_KEY=1
//	same PID as another claimed nickname                  refused (would share its wallet)
//
// PIDs are unchanged, so wallets, friends and private rooms need no migration. The PID is a
// hash into 80 million values, so two different nicknames can, rarely, share one; a claim is
// refused when its PID already belongs to another claimed nickname. Claims live in
// wallets/nick-claims.json (RYUNET_NICKS). A nickname linked to Discord is recovered from any
// installation by its owner (discord_link.go); an unlinked one unused for RYUNET_NICK_EXPIRE_DAYS
// (default 60) is free again; the administrator frees any with the dashboard's
// /api/nick/release?key=<DASH_TOKEN>&name=<nick>. RYUNET_NICK_CLAIMS=0 turns all of it off.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const nickKeyHeader = "X-RyuNET-Key"

// Kept in step with the launcher's ReservedNick and run.py's RESERVED_NICKS.
var reservedNicks = map[string]bool{"ryupc": true, "ryunet": true, "player": true, "jugador": true, "guest": true}

type nickClaim struct {
	KeyHash string `json:"key_hash"` // hex SHA-256 of the installation's secret, never the secret
	Claimed int64  `json:"claimed"`
	// The nickname as its owner writes it ("Neki"; the map key is lower-case). A Discord
	// recovery gives it back to a reinstalled launcher that no longer knows it.
	Name string `json:"name,omitempty"`
	// More installations of the same player: a Discord recovery on another PC adds its key.
	Keys []string `json:"keys,omitempty"`
	// The Discord account (user ID) the nickname is linked to, and its name when last linked
	// (discord_link.go). A linked nickname never expires and is recovered by authorizing
	// Discord again from any installation.
	Discord     string `json:"discord,omitempty"`
	DiscordName string `json:"discord_name,omitempty"`
	LastSeen    int64  `json:"last_seen,omitempty"` // last login, for the expiry of unlinked claims
}

// maxNickKeys bounds the installations of one nickname (the oldest extra key goes first).
const maxNickKeys = 8

func (c *nickClaim) hasKey(h string) bool {
	if c.KeyHash == h {
		return true
	}
	for _, k := range c.Keys {
		if k == h {
			return true
		}
	}
	return false
}

func (c *nickClaim) addKey(h string) {
	if c.hasKey(h) {
		return
	}
	c.Keys = append(c.Keys, h)
	if len(c.Keys) > maxNickKeys-1 {
		c.Keys = c.Keys[len(c.Keys)-(maxNickKeys-1):]
	}
}

type nickRegistry struct {
	mu        sync.Mutex
	path      string
	claims    map[string]*nickClaim // lower-case nickname
	byPID     map[uint64]string     // PID of each claimed nickname -> that nickname
	byDiscord map[string]string     // Discord user ID -> its linked nickname
}

func newNickRegistry(path string) *nickRegistry {
	return &nickRegistry{path: path, claims: map[string]*nickClaim{}, byPID: map[uint64]string{},
		byDiscord: map[string]string{}}
}

// nickExpireDays: an unlinked nickname nobody logged in with for this many days is free again
// (RYUNET_NICK_EXPIRE_DAYS, default 60; 0 keeps them forever). Nicknames linked to Discord
// never expire: their owner can always recover them.
func nickExpireDays() int64 {
	v := os.Getenv("RYUNET_NICK_EXPIRE_DAYS")
	if v == "" {
		return 60
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 60
	}
	return n
}

// liveLocked returns the claim of this nickname, dropping it first when it has expired.
func (r *nickRegistry) liveLocked(lower string) *nickClaim {
	c := r.claims[lower]
	days := nickExpireDays()
	if c == nil || c.Discord != "" || days == 0 {
		return c
	}
	last := c.LastSeen
	if last < c.Claimed {
		last = c.Claimed
	}
	if time.Now().Unix()-last < days*86400 {
		return c
	}
	delete(r.claims, lower)
	if r.byPID[nickPID(lower)] == lower {
		delete(r.byPID, nickPID(lower))
	}
	if err := r.saveLocked(); err != nil {
		log.Printf("[RyuNET Nicks] save: %v", err)
	}
	log.Printf("[RyuNET Nicks] %q expired (unlinked, unused for %d days): free again", lower, days)
	return nil
}

var nicks = newNickRegistry("")

func nickClaimsEnabled() bool { return os.Getenv("RYUNET_NICK_CLAIMS") != "0" }

// nickKeyRequired closes the transition: once (nearly) everyone runs a launcher that sends a
// key, clients without one are refused even on unclaimed nicknames, so no nickname can be
// shared by two people any more.
func nickKeyRequired() bool { return os.Getenv("RYUNET_NICK_REQUIRE_KEY") == "1" }

func nickPID(lower string) uint64 { return calcPS4ID(lower, true) }

// clashLocked reports the claimed nickname whose PID this other nickname would share.
func (r *nickRegistry) clashLocked(lower string) (string, bool) {
	other, ok := r.byPID[nickPID(lower)]
	if !ok || other == lower {
		return "", false
	}
	return other, r.liveLocked(other) != nil
}

func loadNickClaims() {
	path := envOr("RYUNET_NICKS", filepath.Join(envOr("CTR_WALLETS", "wallets"), "nick-claims.json"))
	nicks.mu.Lock()
	defer nicks.mu.Unlock()
	nicks.path = path
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Fatalf("[RyuNET Nicks] %v", err)
		}
		return
	}
	if err := json.Unmarshal(b, &nicks.claims); err != nil {
		// A damaged file must not silently free every nickname.
		log.Fatalf("[RyuNET Nicks] %s: %v", path, err)
	}
	if nicks.claims == nil {
		nicks.claims = map[string]*nickClaim{}
	}
	nicks.byPID = map[uint64]string{}
	nicks.byDiscord = map[string]string{}
	for lower, c := range nicks.claims {
		if c != nil && c.Discord != "" {
			nicks.byDiscord[c.Discord] = lower
		}
		pid := nickPID(lower)
		if other, ok := nicks.byPID[pid]; ok {
			log.Printf("[RyuNET Nicks] WARNING %q and %q share PID %d (claimed before collision checks)", other, lower, pid)
			continue
		}
		nicks.byPID[pid] = lower
	}
	log.Printf("[RyuNET Nicks] %d claimed nickname(s) from %s (key required: %v, Discord linking: %v, unlinked expire after %d days)",
		len(nicks.claims), path, nickKeyRequired(), discordConfigured(), nickExpireDays())
}

// validNick matches what the launcher lets players type: 3 to 16 of [A-Za-z0-9_-].
func validNick(n string) bool {
	if len(n) < 3 || len(n) > 16 {
		return false
	}
	for _, c := range n {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// validKey: the clients send 32 random bytes as 64 hex digits.
func validKey(k string) bool {
	if len(k) != 64 {
		return false
	}
	_, err := hex.DecodeString(k)
	return err == nil
}

func keyHash(k string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(k)))
	return hex.EncodeToString(sum[:])
}

const (
	nickFree     = "free"
	nickYours    = "yours"
	nickTaken    = "taken"
	nickReserved = "reserved"
	nickInvalid  = "invalid"
)

// status reports what admit would do for this nickname and key, without claiming anything.
func (r *nickRegistry) status(nick, key string) string {
	status, _, _ := r.statusInfo(nick, key)
	return status
}

// statusInfo is status plus whether the nickname is linked to Discord (a taken one can then be
// recovered by its owner) and, for its own installations only, the linked Discord name.
func (r *nickRegistry) statusInfo(nick, key string) (status string, linked bool, discordName string) {
	if !validNick(nick) {
		return nickInvalid, false, ""
	}
	lower := strings.ToLower(nick)
	if reservedNicks[lower] {
		return nickReserved, false, ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.liveLocked(lower)
	switch {
	case c == nil:
		if _, clash := r.clashLocked(lower); clash {
			return nickTaken, false, "" // its PID belongs to another player: choose another nickname
		}
		return nickFree, false, ""
	case validKey(key) && c.hasKey(keyHash(key)):
		return nickYours, c.Discord != "", c.DiscordName
	default:
		return nickTaken, c.Discord != "", ""
	}
}

// admit decides whether this nickname may log in with this key, claiming a free nickname for
// a key. The reason is for the server log.
func (r *nickRegistry) admit(nick, key string) (bool, string) {
	if !nickClaimsEnabled() {
		return true, ""
	}
	lower := strings.ToLower(nick)
	if reservedNicks[lower] {
		return false, "reserved nickname"
	}
	if key != "" && !validKey(key) {
		return false, "malformed nickname key"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.liveLocked(lower)
	if c == nil {
		if other, clash := r.clashLocked(lower); clash {
			return false, "nickname has the same player ID as claimed " + strconv.Quote(other)
		}
	}
	if key == "" {
		if c != nil {
			return false, "nickname claimed; this client sends no key"
		}
		if nickKeyRequired() {
			return false, "no nickname key (old launcher) and RYUNET_NICK_REQUIRE_KEY=1"
		}
		return true, "" // a client from before nickname keys, on a free name
	}
	h := keyHash(key)
	if c != nil {
		if !c.hasKey(h) {
			if c.Discord != "" {
				return false, "nickname claimed by another installation (linked to Discord: its owner can recover it)"
			}
			return false, "nickname claimed by another installation"
		}
		// Last use, for the expiry of unlinked nicknames: written at most twice a day.
		if now := time.Now().Unix(); now-c.LastSeen > 12*3600 {
			c.LastSeen = now
			if err := r.saveLocked(); err != nil {
				log.Printf("[RyuNET Nicks] save: %v", err)
			}
		}
		return true, ""
	}
	if !validNick(nick) {
		return false, "invalid nickname"
	}
	r.claims[lower] = &nickClaim{KeyHash: h, Claimed: time.Now().Unix(), LastSeen: time.Now().Unix(), Name: nick}
	if err := r.saveLocked(); err != nil {
		// Without a durable claim the next login could be refused: do not admit on a guess.
		delete(r.claims, lower)
		log.Printf("[RyuNET Nicks] save: %v", err)
		return false, "nickname registry unavailable"
	}
	r.byPID[nickPID(lower)] = lower
	log.Printf("[RyuNET Nicks] %q claimed", nick)
	return true, ""
}

func (r *nickRegistry) saveLocked() error {
	b, err := json.MarshalIndent(r.claims, "", " ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".nick-claims-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, r.path)
}

// handleNickCheck answers GET /ryunet/nick?name=... so the launcher can say "already in use"
// before the game fails to log in with no explanation.
func handleNickCheck(w http.ResponseWriter, r *http.Request) {
	status, linked, discordName := nickFree, false, ""
	if nickClaimsEnabled() {
		status, linked, discordName = nicks.statusInfo(r.URL.Query().Get("name"), r.Header.Get(nickKeyHeader))
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	reply := map[string]any{"status": status, "linked": linked, "discord_link": discordConfigured()}
	if discordName != "" {
		reply["discord"] = discordName // only to the nickname's own installations
	}
	_ = json.NewEncoder(w).Encode(reply)
}
