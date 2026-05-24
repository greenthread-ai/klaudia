package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

// readInput mirrors a tool's input type: struct tags drive schema generation.
type readInput struct {
	FilePath string `json:"file_path" jsonschema:"description=Absolute path to read"`
	Limit    int    `json:"limit,omitempty" jsonschema:"description=Max lines to read"`
}

func TestForGeneratesObjectSchema(t *testing.T) {
	s, err := For[readInput]()
	if err != nil {
		t.Fatalf("For: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(s.Raw, &m); err != nil {
		t.Fatalf("generated schema is not valid JSON: %v", err)
	}
	if m["type"] != "object" {
		t.Errorf("type = %v, want object", m["type"])
	}
	props, ok := m["properties"].(map[string]any)
	if !ok || props["file_path"] == nil {
		t.Errorf("expected file_path property, got %v", m["properties"])
	}
	// The API rejects $ref indirection in tool schemas.
	if strings.Contains(string(s.Raw), "$ref") {
		t.Errorf("schema must not contain $ref: %s", s.Raw)
	}
}

func TestValidateAcceptsAndRejects(t *testing.T) {
	s, err := For[readInput]()
	if err != nil {
		t.Fatalf("For: %v", err)
	}

	if err := s.Validate([]byte(`{"file_path":"/tmp/x","limit":10}`)); err != nil {
		t.Errorf("valid input rejected: %v", err)
	}
	// Wrong type for limit must fail.
	if err := s.Validate([]byte(`{"file_path":"/tmp/x","limit":"ten"}`)); err == nil {
		t.Error("expected type-mismatch input to be rejected")
	}
	// Malformed JSON must fail.
	if err := s.Validate([]byte(`{not json`)); err == nil {
		t.Error("expected malformed JSON to be rejected")
	}
}
