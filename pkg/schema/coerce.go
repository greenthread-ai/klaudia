package schema

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// Coercing string-encoded scalars is the same bargain as ignoring unknown
// properties (see For): the call means exactly one thing, the schema says which
// type that thing has, and rejecting it costs a turn to buy nothing.
//
// Models emit `"offset": "180"` instead of `180` often enough that it is a
// recognisable failure class, and it is worse than it looks: the rejection is
// a pre-exec failure, so a couple of them in a row also trip the repeated-shape
// loop breaker and the model is told to stop using the tool.
//
// The rule is deliberately narrow — the string must parse *exactly* as the
// declared type. Anything with a second meaning is left alone to fail, because
// guessing which half the caller meant is worse than the error:
//
//	"180"      → 180      (nothing else it could be)
//	"180, "    → 180      (one trailing comma: a truncated emission)
//	"180, 200" → rejected (a range? the first of two? ask)
//	"1.5"      → rejected for an integer field
//	"yes"      → rejected for a boolean; only true/false parse
//
// Coerce walks the advertised schema rather than the Go type so that MCP tools,
// whose schemas arrive from the server at runtime, get the same treatment.

// Coerce repairs string-encoded scalars in input so they match the types
// declared by schemaRaw. It returns the repaired JSON and the (sorted) list of
// instance paths it changed; the list is empty when nothing was coerced, in
// which case the returned bytes are the original input.
func Coerce(schemaRaw, input json.RawMessage) (json.RawMessage, []string) {
	if len(schemaRaw) == 0 || len(input) == 0 {
		return input, nil
	}
	var sch, inst any
	if json.Unmarshal(schemaRaw, &sch) != nil || json.Unmarshal(input, &inst) != nil {
		return input, nil
	}
	c := coercer{}
	fixed := c.walk(sch, inst, "")
	if len(c.changed) == 0 {
		return input, nil
	}
	out, err := json.Marshal(fixed)
	if err != nil {
		return input, nil
	}
	sort.Strings(c.changed)
	return out, c.changed
}

type coercer struct {
	changed []string
}

// walk returns value rewritten to match the types node declares, recording
// every path it altered. Unknown keywords are passed over: a node we cannot
// read leaves its subtree untouched, never dropped.
func (c *coercer) walk(node, value any, path string) any {
	obj, ok := node.(map[string]any)
	if !ok {
		return value
	}
	switch v := value.(type) {
	case map[string]any:
		props, _ := obj["properties"].(map[string]any)
		if props == nil {
			return v
		}
		for key, child := range v {
			sub, ok := props[key]
			if !ok {
				continue
			}
			v[key] = c.walk(sub, child, path+"/"+key)
		}
		return v
	case []any:
		items, ok := obj["items"]
		if !ok {
			return v
		}
		for i, child := range v {
			v[i] = c.walk(items, child, path+"/"+strconv.Itoa(i))
		}
		return v
	case string:
		fixed, ok := coerceScalar(declaredTypes(obj), v)
		if !ok {
			return v
		}
		c.changed = append(c.changed, strings.TrimPrefix(path, "/"))
		return fixed
	}
	return value
}

// declaredTypes reads a node's "type", which JSON Schema allows to be either a
// single name or a list ("integer" or ["integer","null"]).
func declaredTypes(node map[string]any) []string {
	switch t := node["type"].(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// coerceScalar converts s to the first declared type it parses exactly as.
// A node that also accepts a string is left alone — the value already matches.
func coerceScalar(types []string, s string) (any, bool) {
	for _, t := range types {
		if t == "string" {
			return nil, false
		}
	}
	trimmed := strings.TrimSpace(s)
	// One trailing comma is the signature of an emission that ran into the next
	// field; anything after it means the string holds more than one value.
	trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, ","))
	if trimmed == "" {
		return nil, false
	}
	for _, t := range types {
		switch t {
		case "integer":
			if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
				return json.Number(strconv.FormatInt(n, 10)), true
			}
		case "number":
			if _, err := strconv.ParseFloat(trimmed, 64); err == nil {
				return json.Number(trimmed), true
			}
		case "boolean":
			switch trimmed {
			case "true":
				return true, true
			case "false":
				return false, true
			}
		}
	}
	return nil, false
}
