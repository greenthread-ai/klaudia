// Package schema replaces the JS app's use of zod: it generates JSON Schema
// from Go structs (for the API's tool input_schema) and validates model-supplied
// inputs against that schema at runtime.
//
//   - Generation: invopop/jsonschema reflects a Go struct (json/jsonschema tags)
//     into a draft 2020-12 schema.
//   - Validation: santhosh-tekuri/jsonschema/v6 compiles the schema once and
//     validates raw inputs, mirroring zod's safeParse / ajv's validate.
package schema

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/invopop/jsonschema"
	jsonschemav6 "github.com/santhosh-tekuri/jsonschema/v6"
)

// Schema is a generated, pre-compiled JSON Schema for one tool's input type.
// Generate it once at tool construction and reuse it for every request.
type Schema struct {
	// Raw is the schema as advertised to the API (input_schema).
	Raw json.RawMessage
	// compiled validates inputs at dispatch time.
	compiled *jsonschemav6.Schema
}

// For reflects type T into a JSON Schema and compiles it for validation.
//
// The Anthropic API expects a self-contained object schema with no top-level
// $ref and inlined definitions, so we configure the reflector accordingly.
func For[T any]() (*Schema, error) {
	r := &jsonschema.Reflector{
		// Inline everything: the API rejects $ref/$defs indirection in tool schemas.
		DoNotReference: true,
		// Treat all fields as required unless they carry `omitempty`/pointer,
		// matching how zod object schemas are typically authored here.
		RequiredFromJSONSchemaTags: false,
		// Drop the auto-added $id/$schema noise; the API only wants the shape.
		Anonymous: true,
	}
	var zero T
	js := r.Reflect(zero)
	js.Version = "" // strip "$schema": "https://json-schema.org/draft/..."
	js.ID = ""

	raw, err := json.Marshal(js)
	if err != nil {
		return nil, fmt.Errorf("marshal generated schema: %w", err)
	}

	compiled, err := compile(raw)
	if err != nil {
		return nil, fmt.Errorf("compile generated schema: %w", err)
	}
	return &Schema{Raw: raw, compiled: compiled}, nil
}

// compile turns raw JSON Schema bytes into a validator.
func compile(raw []byte) (*jsonschemav6.Schema, error) {
	doc, err := jsonschemav6.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	c := jsonschemav6.NewCompiler()
	const resID = "mem://schema.json"
	if err := c.AddResource(resID, doc); err != nil {
		return nil, err
	}
	return c.Compile(resID)
}

// Validate checks raw input JSON against the schema. The returned error is
// suitable for surfacing to the model as a validation failure.
func (s *Schema) Validate(raw json.RawMessage) error {
	inst, err := jsonschemav6.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("input is not valid JSON: %w", err)
	}
	if err := s.compiled.Validate(inst); err != nil {
		return fmt.Errorf("input does not match schema: %w", err)
	}
	return nil
}
