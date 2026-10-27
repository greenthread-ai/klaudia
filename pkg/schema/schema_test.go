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
	// A missing required field must still fail: leniency is about properties we
	// never asked for, not about the ones the tool needs.
	if err := s.Validate([]byte(`{"limit":10}`)); err == nil {
		t.Error("expected missing file_path to be rejected")
	}
}

// Advertise strictly, accept liberally. Raw still tells the model the shape is
// closed, but a property the tool doesn't know about is dropped rather than
// turning an otherwise-valid call into a failure (json.Unmarshal ignores it
// anyway, so the call would have worked).
func TestValidateIgnoresUnknownProperties(t *testing.T) {
	type nested struct {
		Label string `json:"label"`
	}
	type input struct {
		Question string   `json:"question"`
		Options  []nested `json:"options"`
	}
	s, err := For[input]()
	if err != nil {
		t.Fatalf("For: %v", err)
	}

	if !strings.Contains(string(s.Raw), `"additionalProperties":false`) {
		t.Errorf("advertised schema should stay closed: %s", s.Raw)
	}

	cases := []struct {
		name  string
		input string
	}{
		{"extra top-level property", `{"question":"q","description":"stray","options":[{"label":"a"}]}`},
		{"extra nested property", `{"question":"q","options":[{"label":"a","header":"stray"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.Validate([]byte(tc.input)); err != nil {
				t.Errorf("otherwise-valid input rejected: %v", err)
			}
		})
	}
}
