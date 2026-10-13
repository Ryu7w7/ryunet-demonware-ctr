package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCTRObjectStoreReply(t *testing.T) {
	conn := &lobbyConn{}

	// Test task 8: single object from URL
	reqTask8 := []byte("GET https://objectstore.prod.demonware.net/v1/core/publishers/beenox/objects/NetConfig_23_ps4_na.igx/metadata/?client=beenox-crash-ctr-ps4&context=octane_psn&extra=contentURL")
	reply8 := conn.onObjectStore(8, reqTask8)
	if len(reply8) == 0 {
		t.Fatal("empty reply for task 8")
	}

	// Test task 16: batch objects from header
	reqTask16 := []byte(`GET https://objectstore.prod.demonware.net/v1/core/publishers/objects/metadata/?client=beenox-crash-ctr-ps4&context=octane_psn&extra=contentURL
DW-Objectstore-ObjectIDs: [{"name":"GPData-v1.igz","owner":"beenox"},{"name":"NetConfig_23_ps4_na.igx","owner":"beenox"},{"name":"MarketplaceConfig_2_ps4_na.igx","owner":"beenox"}]`)
	reply16 := conn.onObjectStore(16, reqTask16)
	if len(reply16) == 0 {
		t.Fatal("empty reply for task 16")
	}

	// Verify reply16 contains all 3 objects
	replyStr := string(reply16)
	if !strings.Contains(replyStr, "NetConfig_23_ps4_na.igx") {
		t.Fatal("missing NetConfig_23_ps4_na.igx in reply")
	}
	if !strings.Contains(replyStr, "MarketplaceConfig_2_ps4_na.igx") {
		t.Fatal("missing MarketplaceConfig_2_ps4_na.igx in reply")
	}
	if !strings.Contains(replyStr, "GPData-v1.igz") {
		t.Fatal("missing GPData-v1.igz in reply")
	}

	// Verify JSON structure has "objects" array with required fields
	var res ctrObjectsResponse
	// Extract JSON portion (after envelope headers)
	idx := strings.Index(replyStr, `{"objects":`)
	if idx == -1 {
		t.Fatal("missing json root in envelope")
	}
	if err := json.Unmarshal([]byte(replyStr[idx:]), &res); err != nil {
		t.Fatalf("failed to unmarshal objects json: %v", err)
	}
	if len(res.Objects) != 3 {
		t.Fatalf("expected 3 objects, got %d", len(res.Objects))
	}
	for _, o := range res.Objects {
		if o.Checksum == "" || o.ObjectVersion == "" || o.ACL == "" || o.Created == 0 {
			t.Fatalf("incomplete object metadata: %+v", o)
		}
	}
}
