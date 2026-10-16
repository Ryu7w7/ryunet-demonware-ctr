package main

// Nicknames linked to Discord accounts: an identity that survives reinstalling.
//
// A nickname belongs to the installation keys that claimed it (nick_claims.go); a new folder,
// a formatted PC or another PC has a new key and used to lose the nickname for good. Linking
// ties the nickname to the player's Discord account instead:
//
//  1. The launcher opens Discord's OAuth2 page in the browser (scope identify) with a redirect to
//     a loopback port it listens on, and receives an authorization code.
//  2. It posts the code here (POST /ryunet/discord/link: code, redirect_uri, nick) with its
//     X-RyuNET-Key. This server alone holds the application's client secret: it trades the
//     code for a token at Discord and reads the account's user ID.
//  3. The nickname gets that Discord ID. From then on the same Discord account, from any
//     installation, recovers it: the new installation's key is added to the nickname.
//
// One Discord account owns one nickname. Linked nicknames never expire. Configuration (.env):
// DISCORD_CLIENT_SECRET (Developer Portal > OAuth2; without it linking is off),
// DISCORD_CLIENT_ID (default: the launcher's application) and RYUNET_DISCORD_REDIRECTS (the
// loopback redirect URIs registered in the portal, comma-separated; the launcher's by default).

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultDiscordClientID = "1560408801704943708"

// The launcher listens on the first free one of these ports (launcher.cpp kDiscordLinkPorts).
var defaultDiscordRedirects = []string{
	"http://127.0.0.1:53134/discord",
	"http://127.0.0.1:53135/discord",
	"http://127.0.0.1:53136/discord",
}

// discordAPI is Discord's REST base; tests point it at a fake.
var discordAPI = "https://discord.com/api"

func discordConfigured() bool { return os.Getenv("DISCORD_CLIENT_SECRET") != "" }

func discordRedirectAllowed(uri string) bool {
	list := defaultDiscordRedirects
	if v := strings.TrimSpace(os.Getenv("RYUNET_DISCORD_REDIRECTS")); v != "" {
		list = strings.Split(v, ",")
	}
	for _, allowed := range list {
		if strings.TrimSpace(allowed) == uri {
			return true
		}
	}
	return false
}

type discordUser struct {
	ID         string `json:"id"`
	Username   string `json:"username"`
	GlobalName string `json:"global_name"`
}

func (u discordUser) name() string {
	if u.GlobalName != "" {
		return u.GlobalName
	}
	return u.Username
}

var discordHTTP = &http.Client{Timeout: 15 * time.Second}

// discordIdentify trades an authorization code for the Discord account behind it.
func discordIdentify(ctx context.Context, code, redirect string) (discordUser, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirect},
		"client_id":     {envOr("DISCORD_CLIENT_ID", defaultDiscordClientID)},
		"client_secret": {os.Getenv("DISCORD_CLIENT_SECRET")},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, discordAPI+"/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return discordUser{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := discordHTTP.Do(req)
	if err != nil {
		return discordUser{}, err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return discordUser{}, fmt.Errorf("token exchange: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &token); err != nil || token.AccessToken == "" {
		return discordUser{}, errors.New("token exchange: no access token")
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, discordAPI+"/users/@me", nil)
	if err != nil {
		return discordUser{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	resp, err = discordHTTP.Do(req)
	if err != nil {
		return discordUser{}, err
	}
	body, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return discordUser{}, fmt.Errorf("users/@me: HTTP %d", resp.StatusCode)
	}
	var user discordUser
	if err := json.Unmarshal(body, &user); err != nil || user.ID == "" {
		return discordUser{}, errors.New("users/@me: no user ID")
	}
	return user, nil
}

// Outcomes of link (the launcher shows a message for each).
const (
	linkLinked    = "linked"    // linked (or already was) to this Discord account
	linkRecovered = "recovered" // this installation now owns the nickname again
	linkTaken     = "taken"     // another player's nickname (unlinked, or another Discord)
	linkOtherNick = "other_nick" // this Discord account already owns another nickname (now usable here too)
	linkNoNick    = "no_nick"    // recovery: this Discord account has no nickname
	linkOwnNick   = "own_nick"   // recovery: no nickname on the account, but this installation owns one
	linkInvalid   = "invalid"
	linkReserved  = "reserved"
	linkError     = "unavailable"
)

// link ties a nickname to a Discord account for this installation key; see the file comment.
// An empty nick is a recovery: the account's own nickname, whatever it is (a reinstalled
// launcher does not know it). The second result names the account's nickname for
// recovered and other_nick; in both this installation may use it from then on.
func (r *nickRegistry) link(nick, key string, user discordUser) (string, string) {
	if (nick != "" && !validNick(nick)) || !validKey(key) || user.ID == "" {
		return linkInvalid, ""
	}
	lower := strings.ToLower(nick)
	if reservedNicks[lower] {
		return linkReserved, ""
	}
	h := keyHash(key)
	r.mu.Lock()
	defer r.mu.Unlock()
	if other, ok := r.byDiscord[user.ID]; ok && other != lower && r.claims[other] != nil {
		// The account proves it owns that nickname: this installation gets its key.
		c := r.claims[other]
		if !c.hasKey(h) {
			prev := append([]string(nil), c.Keys...)
			c.addKey(h)
			if err := r.saveLocked(); err != nil {
				log.Printf("[RyuNET Nicks] save: %v", err)
				c.Keys = prev
				return linkError, ""
			}
			log.Printf("[RyuNET Nicks] %q recovered through Discord account %s (%s)", other, user.ID, user.name())
		}
		name := c.Name
		if name == "" {
			name = other
		}
		if nick == "" {
			return linkRecovered, name
		}
		return linkOtherNick, name
	}
	if nick == "" {
		// Nothing linked to the account; this installation may still own a nickname (unlinked,
		// or the field was cleared): the most recently used one.
		best, bestSeen := "", int64(-1)
		for lower, c := range r.claims {
			if c != nil && c.hasKey(h) && c.LastSeen > bestSeen {
				best, bestSeen = lower, c.LastSeen
			}
		}
		if best == "" {
			return linkNoNick, ""
		}
		if name := r.claims[best].Name; name != "" {
			return linkOwnNick, name
		}
		return linkOwnNick, best
	}
	c := r.liveLocked(lower)
	var before *nickClaim
	if c != nil {
		prev := *c
		prev.Keys = append([]string(nil), c.Keys...)
		before = &prev
	}
	status := linkLinked
	switch {
	case c == nil:
		if _, clash := r.clashLocked(lower); clash {
			return linkTaken, ""
		}
		now := time.Now().Unix()
		c = &nickClaim{KeyHash: h, Claimed: now, LastSeen: now}
		r.claims[lower] = c
		r.byPID[nickPID(lower)] = lower
	case c.Discord == "" && !c.hasKey(h):
		return linkTaken, "" // someone else's unlinked nickname: only its own installation links it
	case c.Discord != "" && c.Discord != user.ID:
		return linkTaken, ""
	case !c.hasKey(h):
		c.addKey(h) // the same Discord account on a new installation
		status = linkRecovered
	}
	c.Discord, c.DiscordName, c.Name = user.ID, user.name(), nick
	if err := r.saveLocked(); err != nil {
		log.Printf("[RyuNET Nicks] save: %v", err)
		if before == nil {
			delete(r.claims, lower)
			delete(r.byPID, nickPID(lower))
		} else {
			*c = *before
		}
		return linkError, ""
	}
	r.byDiscord[user.ID] = lower
	log.Printf("[RyuNET Nicks] %q %s Discord account %s (%s)", nick, status, user.ID, user.name())
	return status, ""
}

// unlink removes the Discord link of a nickname, for one of its own installations.
func (r *nickRegistry) unlink(nick, key string) bool {
	if !validNick(nick) || !validKey(key) {
		return false
	}
	lower := strings.ToLower(nick)
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.claims[lower]
	if c == nil || c.Discord == "" || !c.hasKey(keyHash(key)) {
		return false
	}
	id := c.Discord
	c.Discord, c.DiscordName = "", ""
	if err := r.saveLocked(); err != nil {
		log.Printf("[RyuNET Nicks] save: %v", err)
		c.Discord = id
		return false
	}
	delete(r.byDiscord, id)
	log.Printf("[RyuNET Nicks] %q unlinked from Discord", nick)
	return true
}

// release frees a nickname whatever its owner (the server's administrator, for a player who
// lost both the installation and the Discord account).
func (r *nickRegistry) release(nick string) bool {
	lower := strings.ToLower(nick)
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.claims[lower]
	if c == nil {
		return false
	}
	delete(r.claims, lower)
	if r.byPID[nickPID(lower)] == lower {
		delete(r.byPID, nickPID(lower))
	}
	if c.Discord != "" && r.byDiscord[c.Discord] == lower {
		delete(r.byDiscord, c.Discord)
	}
	if err := r.saveLocked(); err != nil {
		log.Printf("[RyuNET Nicks] save: %v", err)
		r.claims[lower] = c
		return false
	}
	log.Printf("[RyuNET Nicks] %q released by the administrator", nick)
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// handleDiscordLink answers POST /ryunet/discord/link and /ryunet/discord/unlink; body is the
// form the auth handler already read (nick, code, redirect_uri).
func handleDiscordLink(w http.ResponseWriter, r *http.Request, body []byte) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"status": linkInvalid})
		return
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": linkInvalid})
		return
	}
	nick, key := form.Get("nick"), r.Header.Get(nickKeyHeader)
	if r.URL.Path == "/ryunet/discord/unlink" {
		ok := nicks.unlink(nick, key)
		writeJSON(w, http.StatusOK, map[string]bool{"unlinked": ok})
		return
	}
	if !nickClaimsEnabled() || !discordConfigured() {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"status": linkError,
			"detail": "this server has no Discord linking (DISCORD_CLIENT_SECRET)"})
		return
	}
	code, redirect := form.Get("code"), form.Get("redirect_uri")
	if code == "" || len(code) > 256 || !discordRedirectAllowed(redirect) || (nick != "" && !validNick(nick)) || !validKey(key) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"status": linkInvalid})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	user, err := discordIdentify(ctx, code, redirect)
	if err != nil {
		log.Printf("[RyuNET Nicks] Discord link for %q failed: %v", nick, err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"status": linkError, "detail": "Discord did not accept the authorization"})
		return
	}
	status, other := nicks.link(nick, key, user)
	reply := map[string]string{"status": status, "discord": user.name()}
	if other != "" {
		reply["nick"] = other
	}
	writeJSON(w, http.StatusOK, reply)
}

// handleNickRelease answers the dashboard's /api/nick/release?key=<DASH_TOKEN>&name=<nick>.
func handleNickRelease(token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token == "" || subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("key")), []byte(token)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		name := r.URL.Query().Get("name")
		writeJSON(w, http.StatusOK, map[string]any{"name": name, "released": nicks.release(name)})
	}
}
