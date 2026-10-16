package main

// Server announcements ("Servidor en mantenimiento en 5 minutos") shown to everyone playing.
//
// The administrator posts one through the dashboard:
//
//	/api/announce?key=<DASH_TOKEN>&text=<message>[&minutes=10][&level=info|warn|urgent]
//
// (an empty text clears it). Launchers ask GET /ryunet/announce about every 30 s while the
// game runs and hand a new one to the game, which draws it as a banner over the picture for a
// few seconds: nothing pauses and nothing has to be pressed. Each announcement has an ID, so a
// player sees it once. Kept in memory: a restart clears it.

import (
	"crypto/subtle"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type announcement struct {
	ID      int64  `json:"id"`
	Text    string `json:"text"`
	Level   string `json:"level"`
	Expires int64  `json:"expires"` // unix seconds
}

var (
	announceMu      sync.Mutex
	currentAnnounce announcement
)

const maxAnnounceRunes = 200

func announceLevel(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "warn", "warning", "aviso":
		return "warn"
	case "urgent", "urgente", "error":
		return "urgent"
	default:
		return "info"
	}
}

// setAnnouncement replaces the announcement (an empty text clears it); minutes 1..1440.
func setAnnouncement(text, level string, minutes int, now time.Time) announcement {
	text = strings.Join(strings.Fields(text), " ") // one line, no control spacing
	if utf8.RuneCountInString(text) > maxAnnounceRunes {
		text = string([]rune(text)[:maxAnnounceRunes])
	}
	if minutes < 1 {
		minutes = 10
	} else if minutes > 1440 {
		minutes = 1440
	}
	announceMu.Lock()
	defer announceMu.Unlock()
	if text == "" {
		currentAnnounce = announcement{}
		return currentAnnounce
	}
	id := now.UnixNano()
	if id <= currentAnnounce.ID {
		id = currentAnnounce.ID + 1
	}
	currentAnnounce = announcement{ID: id, Text: text, Level: announceLevel(level), Expires: now.Add(time.Duration(minutes) * time.Minute).Unix()}
	return currentAnnounce
}

// activeAnnouncement is the announcement while it lasts, else the zero one.
func activeAnnouncement(now time.Time) announcement {
	announceMu.Lock()
	defer announceMu.Unlock()
	if currentAnnounce.ID == 0 || now.Unix() >= currentAnnounce.Expires {
		return announcement{}
	}
	return currentAnnounce
}

// handleAnnounce answers the launchers' GET /ryunet/announce:
// {"id":..,"text":..,"level":..,"expires_in":seconds} or {"id":0}.
func handleAnnounce(w http.ResponseWriter, _ *http.Request) {
	now := time.Now()
	a := activeAnnouncement(now)
	if a.ID == 0 {
		writeJSON(w, http.StatusOK, map[string]int64{"id": 0})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": a.ID, "text": a.Text, "level": a.Level, "expires_in": a.Expires - now.Unix()})
}

// handleAnnounceAdmin answers the dashboard's /api/announce (see the file comment).
func handleAnnounceAdmin(token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token == "" || subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("key")), []byte(token)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		q := r.URL.Query()
		minutes, _ := strconv.Atoi(q.Get("minutes"))
		a := setAnnouncement(q.Get("text"), q.Get("level"), minutes, time.Now())
		if a.ID == 0 {
			log.Printf("[RyuNET Announce] cleared")
		} else {
			log.Printf("[RyuNET Announce] %s until %s: %s", a.Level, time.Unix(a.Expires, 0).Format("15:04"), a.Text)
		}
		writeJSON(w, http.StatusOK, a)
	}
}
