package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAnnouncementLifecycle(t *testing.T) {
	defer setAnnouncement("", "", 0, time.Now())
	now := time.Unix(1_800_000_000, 0)
	a := setAnnouncement("  Servidor en\nmantenimiento   en 5 minutos ", "aviso", 5, now)
	if a.ID == 0 || a.Text != "Servidor en mantenimiento en 5 minutos" || a.Level != "warn" || a.Expires != now.Unix()+300 {
		t.Fatalf("announcement: %+v", a)
	}
	if got := activeAnnouncement(now.Add(4 * time.Minute)); got.ID != a.ID {
		t.Fatal("not active before it expires")
	}
	if got := activeAnnouncement(now.Add(5 * time.Minute)); got.ID != 0 {
		t.Fatal("still active after it expired")
	}
	b := setAnnouncement("Reinicio ahora", "urgente", 0, now)
	if b.ID <= a.ID || b.Level != "urgent" || b.Expires != now.Unix()+600 {
		t.Fatalf("second announcement: %+v (previous id %d)", b, a.ID)
	}
	if long := setAnnouncement(strings.Repeat("ñ", 300), "", 1, now); len([]rune(long.Text)) != maxAnnounceRunes {
		t.Fatalf("not truncated: %d runes", len([]rune(long.Text)))
	}
	if cleared := setAnnouncement("", "", 0, now); cleared.ID != 0 || activeAnnouncement(now).ID != 0 {
		t.Fatal("empty text did not clear it")
	}
}

func TestAnnouncementEndpoints(t *testing.T) {
	defer setAnnouncement("", "", 0, time.Now())
	admin := handleAnnounceAdmin("tok")
	rec := httptest.NewRecorder()
	admin(rec, httptest.NewRequest(http.MethodGet, "/api/announce?key=wrong&text=x", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong token: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	admin(rec, httptest.NewRequest(http.MethodGet, "/api/announce?key=tok&text=Mantenimiento+en+5+minutos&minutes=5&level=warn", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	handleAnnounce(rec, httptest.NewRequest(http.MethodGet, "/ryunet/announce", nil))
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["text"] != "Mantenimiento en 5 minutos" || got["level"] != "warn" || got["id"].(float64) == 0 || got["expires_in"].(float64) <= 0 {
		t.Fatalf("public: %v", got)
	}
}
