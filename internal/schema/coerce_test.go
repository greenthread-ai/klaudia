package schema

import (
	"encoding/json"
	"testing"
)

func TestCoerceStringEncodedScalars(t *testing.T) {
	type input struct {
		FilePath string  `json:"file_path"`
		Offset   int     `json:"offset,omitempty"`
		Ratio    float64 `json:"ratio,omitempty"`
		All      bool    `json:"all,omitempty"`
	}
	s, err := For[input]()
	if err != nil {
		t.Fatalf("For: %v", err)
	}

	cases := []struct {
		name    string
		in      string
		want    string
		changed bool
	}{
		{"integer as string", `{"file_path":"/tmp/x","offset":"180"}`, `{"file_path":"/tmp/x","offset":180}`, true},
		{"one trailing comma", `{"file_path":"/tmp/x","offset":"180, "}`, `{"file_path":"/tmp/x","offset":180}`, true},
		{"number as string", `{"file_path":"/tmp/x","ratio":"1.5"}`, `{"file_path":"/tmp/x","ratio":1.5}`, true},
		{"boolean as string", `{"file_path":"/tmp/x","all":"true"}`, `{"file_path":"/tmp/x","all":true}`, true},
		{"already typed", `{"file_path":"/tmp/x","offset":180}`, `{"file_path":"/tmp/x","offset":180}`, false},
		// A string field keeps its string, however numeric it looks.
		{"numeric string field", `{"file_path":"42"}`, `{"file_path":"42"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := Coerce(s.Raw, []byte(tc.in))
			if (len(changed) > 0) != tc.changed {
				t.Errorf("changed = %v, want %v", changed, tc.changed)
			}
			if !sameJSON(t, got, []byte(tc.want)) {
				t.Errorf("Coerce = %s, want %s", got, tc.want)
			}
			if err := s.Validate(got); err != nil {
				t.Errorf("coerced input still rejected: %v", err)
			}
		})
	}
}

// Anything with a second reading is left to fail. Guessing which half of
// "180, 200" was meant is worse than the error message.
func TestCoerceLeavesAmbiguousValues(t *testing.T) {
	type input struct {
		Offset int  `json:"offset,omitempty"`
		All    bool `json:"all,omitempty"`
	}
	s, err := For[input]()
	if err != nil {
		t.Fatalf("For: %v", err)
	}

	for _, in := range []string{
		`{"offset":"180, 200"}`,
		`{"offset":"1.5"}`,
		`{"offset":"about 180"}`,
		`{"offset":""}`,
		`{"all":"yes"}`,
	} {
		got, changed := Coerce(s.Raw, []byte(in))
		if len(changed) > 0 {
			t.Errorf("%s: coerced %v, want untouched", in, changed)
		}
		if string(got) != in {
			t.Errorf("%s: rewritten to %s", in, got)
		}
		if err := s.Validate(got); err == nil {
			t.Errorf("%s: expected validation to reject it", in)
		}
	}
}

// Nested objects and arrays reach the same path, which is what makes the fix
// cover AskUserQuestion-shaped inputs and not just flat ones.
func TestCoerceNested(t *testing.T) {
	type opt struct {
		Label   string `json:"label"`
		Default bool   `json:"default,omitempty"`
	}
	type input struct {
		Question string `json:"question"`
		Options  []opt  `json:"options"`
	}
	s, err := For[input]()
	if err != nil {
		t.Fatalf("For: %v", err)
	}

	got, changed := Coerce(s.Raw, []byte(`{"question":"q","options":[{"label":"a"},{"label":"b","default":"true"}]}`))
	if len(changed) != 1 || changed[0] != "options/1/default" {
		t.Errorf("changed = %v, want [options/1/default]", changed)
	}
	if !sameJSON(t, got, []byte(`{"question":"q","options":[{"label":"a"},{"label":"b","default":true}]}`)) {
		t.Errorf("Coerce = %s", got)
	}
}

// An MCP server's schema arrives as raw JSON with no Go type behind it; the
// walk is driven by the schema, so it works the same way.
func TestCoerceRawSchema(t *testing.T) {
	raw := []byte(`{"type":"object","properties":{"limit":{"type":["integer","null"]},"name":{"type":"string"}}}`)
	got, changed := Coerce(raw, []byte(`{"name":"x","limit":"25"}`))
	if len(changed) != 1 || changed[0] != "limit" {
		t.Fatalf("changed = %v, want [limit]", changed)
	}
	if !sameJSON(t, got, []byte(`{"name":"x","limit":25}`)) {
		t.Errorf("Coerce = %s", got)
	}
}

func TestCoerceIgnoresJunk(t *testing.T) {
	if got, changed := Coerce(nil, []byte(`{"a":"1"}`)); len(changed) > 0 || string(got) != `{"a":"1"}` {
		t.Errorf("no schema: got %s %v", got, changed)
	}
	if got, changed := Coerce([]byte(`{"type":"object"}`), []byte(`{not json`)); len(changed) > 0 || string(got) != `{not json` {
		t.Errorf("bad input: got %s %v", got, changed)
	}
}

func sameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatalf("unmarshal %s: %v", a, err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}
	ax, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return string(ax) == string(by)
}
