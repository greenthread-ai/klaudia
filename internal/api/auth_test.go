package api

import (
	"encoding/json"
	"testing"
)

func TestMergeOAuthPayloadPreservesUnknownFields(t *testing.T) {
	// A realistic keychain payload with extra fields Klaudia doesn't model.
	current := []byte(`{
		"claudeAiOauth": {
			"accessToken": "old-access",
			"refreshToken": "old-refresh",
			"expiresAt": 111,
			"scopes": ["user:inference","user:profile"],
			"subscriptionType": "max"
		},
		"someOtherTopLevel": {"keep": true}
	}`)

	out, err := mergeOAuthPayload(current, refreshedTokens{
		AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresAt: 999,
	})
	if err != nil {
		t.Fatal(err)
	}

	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	oauth := m["claudeAiOauth"].(map[string]any)

	// Updated fields.
	if oauth["accessToken"] != "new-access" || oauth["refreshToken"] != "new-refresh" {
		t.Errorf("tokens not updated: %v", oauth)
	}
	if oauth["expiresAt"].(float64) != 999 {
		t.Errorf("expiresAt = %v, want 999", oauth["expiresAt"])
	}
	// Preserved fields (the whole point).
	if oauth["subscriptionType"] != "max" {
		t.Errorf("dropped subscriptionType: %v", oauth)
	}
	if _, ok := oauth["scopes"]; !ok {
		t.Errorf("dropped scopes: %v", oauth)
	}
	if top, ok := m["someOtherTopLevel"].(map[string]any); !ok || top["keep"] != true {
		t.Errorf("dropped top-level field: %v", m)
	}
}
