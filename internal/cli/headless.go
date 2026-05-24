// Package cli implements the command-line surface: flag parsing (cobra) and
// the headless (-p) output renderers for text / json / stream-json.
//
// The event and result shapes mirror the JS reference (07-app-features.js result
// emission) so the differential test harness can diff Go output against
// `node dist/cli.js -p ... --output-format <fmt>`.
package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

// OutputFormat selects how headless results are rendered to stdout.
type OutputFormat string

const (
	FormatText       OutputFormat = "text"
	FormatJSON       OutputFormat = "json"
	FormatStreamJSON OutputFormat = "stream-json"
)

// ParseOutputFormat validates a --output-format value.
func ParseOutputFormat(s string) (OutputFormat, error) {
	switch OutputFormat(s) {
	case FormatText, FormatJSON, FormatStreamJSON:
		return OutputFormat(s), nil
	default:
		return "", fmt.Errorf("invalid output format %q (want text|json|stream-json)", s)
	}
}

// ResultMessage is the terminal "result" event of a headless run. Field names
// and JSON tags match the JS bundle (07-app-features.js:33746) so JSON output
// is byte-compatible after timestamp/UUID normalization.
type ResultMessage struct {
	Type          string         `json:"type"` // always "result"
	Subtype       string         `json:"subtype"`
	IsError       bool           `json:"is_error"`
	DurationMS    int64          `json:"duration_ms"`
	DurationAPIMS int64          `json:"duration_api_ms"`
	NumTurns      int            `json:"num_turns"`
	Result        string         `json:"result"`
	StopReason    string         `json:"stop_reason,omitempty"`
	SessionID     string         `json:"session_id"`
	TotalCostUSD  float64        `json:"total_cost_usd"`
	Usage         map[string]any `json:"usage,omitempty"`
	UUID          string         `json:"uuid"`
}

// Renderer writes headless output in the configured format.
type Renderer struct {
	format OutputFormat
	w      io.Writer
}

// NewRenderer builds a Renderer for the given format and sink.
func NewRenderer(format OutputFormat, w io.Writer) *Renderer {
	return &Renderer{format: format, w: w}
}

// Event emits a streaming event line (used only by stream-json). For text/json
// formats intermediate events are suppressed; the final result is emitted by Result.
func (r *Renderer) Event(ev any) error {
	if r.format != FormatStreamJSON {
		return nil
	}
	return r.writeJSONLine(ev)
}

// Result emits the terminal result in the configured format:
//   - text:        the result string + newline
//   - json:        a single ResultMessage object
//   - stream-json: a final result event line
func (r *Renderer) Result(res ResultMessage) error {
	switch r.format {
	case FormatText:
		_, err := fmt.Fprintln(r.w, res.Result)
		return err
	case FormatJSON, FormatStreamJSON:
		return r.writeJSONLine(res)
	default:
		return fmt.Errorf("unknown output format %q", r.format)
	}
}

func (r *Renderer) writeJSONLine(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(r.w, string(b))
	return err
}
