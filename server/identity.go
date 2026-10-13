package main

// Player identity read from the authentication request.
//
// Switch: extra_data (JSON string) carries "username" (Nintendo nickname) and
// "token" (NSA id_token). The token's "nnex" claim is "nx2.<b64>.<sig>",
// where <b64> decodes to "<Nextendo PID>.<nickname>.<expiry>". This PID is
// the player's Nextendo account; Citron also uses it as its friend identifier
// (seen: player1 = 1800000101). Signature checked in gates.go.
//
// PS4 (lavender, CUSA13795, shadPS4+shadNet): extra_data is still a JSON string
// but carries PSN data. The emulator/shadNet sends either:
//   - plain "username" + "token" containing NPID (numeric) or base64 NP ticket,
//   - or "npId"/"onlineId"/"psnId" fields.
// For localhost PoC we accept any of them and derive a stable PID:
//   1) try Nintendo nnex path first (backwards compat with Switch)
//   2) fallback to PS4 fields -> numeric NPID -> PID
//   3) fallback to deterministic hash of username (so guests without NPID still get a stable ID)
// Device kind: "switch"/"ryujinx" for Switch, "ps4"/"shadps4" for PS4.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func b64any(s string) []byte {
	s = strings.TrimRight(s, "=")
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b
	}
	return nil
}

func isHexIdentity(s string) bool {
	if len(s) < 4 || len(s) > 16 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// playerIdentity returns the player identity and the raw nnex claim ("" if missing).
func playerIdentity(body []byte) (playerID, string) {
	id := playerID{Kind: "switch"}
	var req struct {
		ExtraData string `json:"extra_data"`
		TitleID   string `json:"title_id"`
		Identity  string `json:"identity"`
	}
	if json.Unmarshal(body, &req) != nil {
		return id, ""
	}
	// PS4 CTR is title 5760, Switch CTR is 5775 (both need stable PID). EBOOT CUSA13795 shows title_id 5760.
	isPS4Title := req.TitleID == "5775" || req.TitleID == "5760"
	_ = isPS4Title

	var extra struct {
		Username     string `json:"username"`
		Token        string `json:"token"`
		// PS4/shadNet extensions (optional, all strings to tolerate numbers)
		NpId         string `json:"npId"`
		OnlineId     string `json:"onlineId"`
		PsnId        string `json:"psnId"`
		Npid         string `json:"npid"`
		Version      string `json:"version"`
		ExtendedData any    `json:"extended_data"`
		// extended_data is also forwarded by Switch; ignore here
	}
	// extra_data may be malformed or PS4-style; try to parse, but don't fail if it doesn't match
	_ = json.Unmarshal([]byte(req.ExtraData), &extra)
	// PS4 extra_data token is itself a JSON string: {"scope":"...","id_token":"TOKEN"}
	// If Token looks like JSON, try to extract id_token inside
	if extra.Token != "" && strings.HasPrefix(strings.TrimSpace(extra.Token), "{") {
		var inner struct {
			IdToken string `json:"id_token"`
			Scope   string `json:"scope"`
		}
		// The Token string is JSON-escaped inside extra_data, but json.Unmarshal already unescaped it
		if json.Unmarshal([]byte(extra.Token), &inner) == nil {
			// If id_token is placeholder "TOKEN", ignore; else treat as username/candidate
			if inner.IdToken != "" && inner.IdToken != "TOKEN" {
				extra.Token = inner.IdToken
				if extra.Username == "" {
					extra.Username = inner.IdToken
				}
			} else if inner.IdToken == "TOKEN" {
				// Placeholder - clear token so we don't hash "TOKEN" string
				extra.Token = ""
			}
		}
	}
	if extra.Username != "" {
		id.Username = extra.Username
	}
	// Also try generic PS4 username fields if Username was empty
	if id.Username == "" {
		if extra.OnlineId != "" {
			id.Username = extra.OnlineId
		} else if extra.NpId != "" {
			id.Username = extra.NpId
		} else if extra.PsnId != "" {
			id.Username = extra.PsnId
		} else if extra.Token != "" && !strings.Contains(extra.Token, ".") {
			id.Username = extra.Token
		}
	}

	// 1) Try Nintendo path first (backwards compat)
	parts := strings.Split(extra.Token, ".")
	if len(parts) >= 2 {
		var claims struct {
			Sub  string          `json:"sub"`
			Nnex string          `json:"nnex"`
			DI   json.RawMessage `json:"di"`
			SN   json.RawMessage `json:"sn"`
		}
		if json.Unmarshal(b64any(parts[1]), &claims) == nil && claims.Nnex != "" {
			id.Sub = claims.Sub
			if len(claims.DI) > 0 || len(claims.SN) > 0 {
				id.Kind = "ryujinx"
			}
			nn := strings.TrimPrefix(claims.Nnex, "nx2.")
			if i := strings.IndexByte(nn, '.'); i > 0 {
				nn = nn[:i]
			}
			if dec := string(b64any(nn)); dec != "" {
				f := strings.SplitN(dec, ".", 3)
				if pid, err := strconv.ParseUint(f[0], 10, 64); err == nil {
					id.PID = pid
				}
				if id.Username == "" && len(f) > 1 {
					id.Username = f[1]
				}
			}
			if id.PID != 0 {
				return id, claims.Nnex
			}
		}
	}

	// PS4 CTR: if we have a valid username (from id_token, extra.Username, etc.), derive a deterministic PID
	if id.Username != "" && !strings.EqualFold(id.Username, "356c4bc3") {
		h := fnv.New64a()
		h.Write([]byte(strings.ToLower(id.Username)))
		hash := h.Sum64()
		sha := sha256.Sum256([]byte(strings.ToLower(id.Username)))
		mix := binary.LittleEndian.Uint64(sha[:8]) ^ hash
		pid := 1810000000 + (mix % 80000000)
		if pid == 0 {
			pid = 1810000001
		}
		id.PID = pid
		id.Kind = "shadps4"
		if isPS4Title {
			id.Kind = "ps4"
		}
		return id, ""
	}

	// 2) PS4/shadNet fallback: extract numeric NPID from any field (excluding fixed CTR identity hex "356c4bc3")
	candidates := []string{extra.NpId, extra.Npid, extra.OnlineId, extra.PsnId, extra.Token, extra.Username}
	for _, c := range candidates {
		if c == "" || strings.EqualFold(c, "356c4bc3") {
			continue
		}
		// Hex identity?
		if isHexIdentity(c) {
			if pid, err := strconv.ParseUint(strings.TrimSpace(c), 16, 64); err == nil && pid != 0 {
				id.PID = pid
				if id.Username == "" || id.Username == c {
					id.Username = c
				}
				id.Kind = "shadps4"
				return id, ""
			}
		}
		// Direct numeric?
		if pid, err := strconv.ParseUint(strings.TrimSpace(c), 10, 64); err == nil && pid != 0 {
			id.PID = pid
			id.Kind = "shadps4"
			return id, ""
		}
		// Try to find numeric substring (e.g. "NP1234567890" or base64 ticket containing PID)
		if m := regexp.MustCompile(`\d{5,19}`).FindString(c); m != "" {
			if pid, err := strconv.ParseUint(m, 10, 64); err == nil && pid != 0 && pid < 1<<62 {
				// Heuristic: avoid picking expiry timestamps (>1e12)
				if pid < 8000000000000 {
					id.PID = pid
					id.Kind = "shadps4"
					return id, ""
				}
			}
		}
		// Try base64 decode and look for PID inside
		if dec := b64any(c); len(dec) > 4 {
			if m := regexp.MustCompile(`\d{5,19}`).FindString(string(dec)); m != "" {
				if pid, err := strconv.ParseUint(m, 10, 64); err == nil && pid != 0 {
					id.PID = pid
					id.Kind = "shadps4"
					return id, ""
				}
			}
		}
	}

	// 3) Fallback: deterministic PID from username/identity (stable across reconnects)
	fallbackName := id.Username
	if fallbackName == "" && extra.Username != "" {
		fallbackName = extra.Username
	}
	if fallbackName == "" && extra.OnlineId != "" {
		fallbackName = extra.OnlineId
	}
	if fallbackName == "" && extra.Version != "" {
		fallbackName = "player-" + strconv.FormatInt(time.Now().UnixNano()%10000, 10)
	}
	if fallbackName != "" && !strings.EqualFold(fallbackName, "356c4bc3") {
		h := fnv.New64a()
		h.Write([]byte(strings.ToLower(fallbackName)))
		hash := h.Sum64()
		sha := sha256.Sum256([]byte(strings.ToLower(fallbackName)))
		mix := binary.LittleEndian.Uint64(sha[:8]) ^ hash
		pid := 1810000000 + (mix % 80000000)
		if pid == 0 {
			pid = 1810000001
		}
		id.PID = pid
		if id.Username == "" {
			id.Username = fallbackName
		}
		id.Kind = "shadps4"
		if isPS4Title {
			id.Kind = "ps4"
		}
		return id, ""
	}

	// Final fallback for PS4 CTR: unique guest PID
	if isPS4Title {
		randPart, _ := rand.Prime(rand.Reader, 24)
		guestPID := uint64(1810000000)
		if randPart != nil {
			guestPID += randPart.Uint64() % 80000000
		}
		id.PID = guestPID
		id.Username = fmt.Sprintf("Player_%d", guestPID%10000)
		id.Kind = "ps4"
		return id, ""
	}

	return id, ""
}
