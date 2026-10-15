package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The client encodes its integers as JSON strings, so accept both forms.
type jsonUint32 uint32

func (v *jsonUint32) UnmarshalJSON(b []byte) error {
	n, err := strconv.ParseUint(strings.Trim(string(b), `"`), 10, 32)
	if err != nil {
		return err
	}
	*v = jsonUint32(n)
	return nil
}

// Umbrella is a separate API from /auth/. CTR's bdUmbrellaUserAccount
// parser expects umbrellaID, accessToken, expires and accounts.
func handleCTRUmbrella(w http.ResponseWriter, r *http.Request, body []byte, n uint64, dumps string) {
	deny := func(code int, reason string) {
		preview := body
		if len(preview) > 300 {
			preview = preview[:300]
		}
		log.Printf("[CTR Umbrella] #%d %s -> %d (%s) body=%s", n, r.URL.RequestURI(), code, reason, preview)
		http.Error(w, reason, code)
	}
	if dumps != "" && len(body) > 0 {
		if err := os.WriteFile(filepath.Join(dumps, fmt.Sprintf("%03d_umbrella.json", n)), body, 0o644); err != nil {
			log.Printf("[CTR Umbrella] dump: %v", err)
		}
	}
	if r.Method != http.MethodPost {
		deny(405, "method not allowed")
		return
	}
	var req struct {
		Ticket  string     `json:"ticket"`
		TitleID jsonUint32 `json:"titleID"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		deny(400, fmt.Sprintf("unreadable request: %v", err))
		return
	}
	if req.TitleID != 5775 && req.TitleID != 5760 {
		deny(400, fmt.Sprintf("unexpected title %d", req.TitleID))
		return
	}
	ticket, err := base64.StdEncoding.DecodeString(req.Ticket)
	if err != nil || len(ticket) != 128 || binary.LittleEndian.Uint32(ticket[:4]) != ticketMagic ||
		(binary.LittleEndian.Uint32(ticket[5:9]) != uint32(req.TitleID)) {
		deny(401, fmt.Sprintf("invalid ticket (decoded %d bytes, err %v)", len(ticket), err))
		return
	}
	// Accept only a ticket issued by this private server, not arbitrary claims.
	var id *playerID
	if pid, ok := issuedTicketPlayer(ticket, time.Now(), 24*time.Hour); ok && pid != nil && pid.PID != 0 {
		id = pid
	} else {
		// Fallback for tests or legacy tickets on disk
		files, _ := filepath.Glob(filepath.Join(sessDir, "*.tkt"))
		issued := false
		for _, f := range files {
			b, e := os.ReadFile(f)
			if e == nil && bytes.Equal(b, ticket) {
				issued = true
				break
			}
		}
		if !issued {
			deny(401, fmt.Sprintf("ticket not issued here (%d on file)", len(files)))
			return
		}
		if issuedAt := binary.LittleEndian.Uint32(ticket[13:17]); time.Since(time.Unix(int64(issuedAt), 0)) > 24*time.Hour {
			deny(401, fmt.Sprintf("ticket issued at %d is too old", issuedAt))
			return
		}
		raw, err := os.ReadFile(filepath.Join(sessDir, fmt.Sprintf("id_%x.json", ticket[33:41])))
		var diskID playerID
		if err != nil || json.Unmarshal(raw, &diskID) != nil || diskID.PID == 0 {
			deny(401, fmt.Sprintf("unknown account: %v", err))
			return
		}
		id = &diskID
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		deny(500, "token generation failed")
		return
	}
	debugf("[CTR Umbrella] #%d pid=%d -> 200 (lsg token issued)", n, id.PID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		ID       uint64 `json:"umbrellaID"`
		Token    string `json:"accessToken"`
		Expires  int64  `json:"expires"`
		Accounts []any  `json:"accounts"`
	}{id.PID, base64.RawURLEncoding.EncodeToString(token), time.Now().Add(time.Hour).Unix(), []any{}})
}
