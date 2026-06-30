package gateway_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestServerEvaluatesCommandPolicy(t *testing.T) {
	t.Parallel()

	server := newTestServer(t)

	allowResponse := doJSON(t, server, http.MethodPost, "/api/command-policy/evaluate", map[string]string{
		"command": "go test ./...",
	})
	if allowResponse.Code != http.StatusOK {
		t.Fatalf("expected allow response status 200, got %d", allowResponse.Code)
	}
	var allowDecision map[string]any
	if err := json.NewDecoder(allowResponse.Body).Decode(&allowDecision); err != nil {
		t.Fatalf("decode allow decision: %v", err)
	}
	if allowDecision["action"] != "allow" {
		t.Fatalf("expected allow decision, got %v", allowDecision["action"])
	}

	blockResponse := doJSON(t, server, http.MethodPost, "/api/command-policy/evaluate", map[string]string{
		"command": "type C:\\Users\\Sandeep\\.ssh\\id_rsa",
	})
	if blockResponse.Code != http.StatusOK {
		t.Fatalf("expected block response status 200, got %d", blockResponse.Code)
	}
	var blockDecision map[string]any
	if err := json.NewDecoder(blockResponse.Body).Decode(&blockDecision); err != nil {
		t.Fatalf("decode block decision: %v", err)
	}
	if blockDecision["action"] != "block" {
		t.Fatalf("expected block decision, got %v", blockDecision["action"])
	}
}
