package main

import (
	"encoding/json"
	"strings"
)

const svcObjectStore = 193

type ctrObjectMetadata struct {
	Name          string `json:"name"`
	Owner         string `json:"owner"`
	Context       string `json:"context"`
	ObjectVersion string `json:"objectVersion"`
	Checksum      string `json:"checksum"`
	ContentLength int64  `json:"contentLength"`
	Created       int64  `json:"created"`
	Modified      int64  `json:"modified"`
	ExpiresOn     int64  `json:"expiresOn"`
	ACL           string `json:"acl"`
	Category      string `json:"category,omitempty"`
}

type ctrObjectsResponse struct {
	Objects []ctrObjectMetadata `json:"objects"`
}

func makeObjectMetadata(name string) ctrObjectMetadata {
	return ctrObjectMetadata{
		Name:          name,
		Owner:         "beenox",
		Context:       "octane_psn",
		ObjectVersion: "1",
		Checksum:      "00000000000000000000000000000000",
		ContentLength: 0,
		Created:       1560000000,
		Modified:      1560000000,
		ExpiresOn:     2000000000,
		ACL:           "public",
		Category:      "config",
	}
}

func (l *lobbyConn) onObjectStore(task byte, payload []byte) []byte {
	var objects []ctrObjectMetadata

	// 1. Check if DW-Objectstore-ObjectIDs JSON array is present in the payload (task 16 batch)
	if idx := strings.Index(string(payload), `[{"name":`); idx != -1 {
		end := strings.Index(string(payload[idx:]), `]`)
		if end != -1 {
			jsonSlice := payload[idx : idx+end+1]
			var list []struct {
				Name  string `json:"name"`
				Owner string `json:"owner"`
			}
			if err := json.Unmarshal(jsonSlice, &list); err == nil && len(list) > 0 {
				for _, item := range list {
					objects = append(objects, makeObjectMetadata(item.Name))
				}
			}
		}
	}

	// 2. If no objects found from header, check if URL has /objects/{name}/metadata/ (task 8 single)
	if len(objects) == 0 {
		strPayload := string(payload)
		if objIdx := strings.Index(strPayload, "/objects/"); objIdx != -1 {
			rest := strPayload[objIdx+len("/objects/"):]
			if metaIdx := strings.Index(rest, "/metadata/"); metaIdx != -1 {
				name := rest[:metaIdx]
				if !strings.Contains(name, "/") && len(name) > 0 {
					objects = append(objects, makeObjectMetadata(name))
				}
			}
		}
	}

	// 3. Fallback: default known configuration files
	if len(objects) == 0 {
		objects = []ctrObjectMetadata{
			makeObjectMetadata("NetConfig_23_ps4_na.igx"),
			makeObjectMetadata("MarketplaceConfig_2_ps4_na.igx"),
			makeObjectMetadata("GPData-v1.igz"),
		}
	}

	resp := ctrObjectsResponse{Objects: objects}
	body, _ := json.Marshal(resp)
	l.debugf("task bdObjectStore task=%d -> HTTP 200 (%d objects, %d bytes)", task, len(objects), len(body))
	return httpProxyReply(task, 200, body)
}
