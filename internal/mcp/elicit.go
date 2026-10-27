package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

// Elicitation (`elicitation/create`) is how a server asks the *user* for input
// in the middle of a tool call — an API key it has no other way to obtain, a
// branch name, a yes/no on something destructive. Without a handler the SDK
// advertises no elicitation capability and a server that depends on it either
// takes a degraded path or fails, and the failure says nothing about why.
//
// On protocol 2026-07-28 a server cannot send elicitation/create at all while
// serving a request; it returns an InputRequests map in the tool result and the
// client fulfils it and retries (multi round-trip requests, SEP-2322). The
// SDK's client middleware runs that loop, so nothing here drives it — but it is
// why the handler has to be installed on the client at connect time rather than
// consulted at call time, and why the Asker arrives through a field.
//
// Only "form" elicitation is supported, and only form is advertised. The other
// mode in the spec is "url": the client is handed a link, shows it, answers
// immediately, and learns the outcome later from an `elicitation/complete`
// notification. A terminal cannot open a browser without touching the host, and
// answering "accept" the instant a URL arrives would claim the user had seen a
// link that had only been printed into a scrollback they may not be looking at.
// Declaring form-only lets a server pick its own fallback instead of being told
// yes and then stranded. Nothing on the multi round-trip path enforces the
// declared mode, so the check in elicit is the one that actually holds.
//
// Values arrive as free text, so a field's declared type is a coercion that can
// fail. A failure cancels the whole elicitation rather than guessing: "cancel"
// is recoverable — the server may ask again — where an invented number is not.

// Elicitor answers elicitation requests by putting them to the user through an
// Asker.
//
// The Asker is a field set per turn, not a constructor argument, because the
// two have different lifetimes: MCP clients are built once at startup (and
// again on a config reload), while the frontend hands the loop a fresh Asker
// for every turn. A client built at startup therefore cannot close over the
// Asker it will need; it closes over this instead.
//
// A nil Elicitor means no elicitation capability is advertised at all, which is
// the right answer for a headless run — there is nobody to ask, and a server
// told otherwise would take the interactive path and then be declined.
type Elicitor struct {
	mu  sync.RWMutex
	ask tools.Asker

	// askMu is held across the whole conversation with the user, not just
	// around reading ask. One round of multi round-trip can carry several input
	// requests, and the SDK fulfils them concurrently (an errgroup in mrtr.go).
	// The frontend has a single question slot: two prompts in flight at once
	// overwrite each other's options and the user answers the wrong question.
	askMu sync.Mutex
}

// NewElicitor returns an Elicitor with no Asker yet; see SetAsker.
func NewElicitor() *Elicitor { return &Elicitor{} }

// SetAsker installs the Asker that subsequent elicitations go to. The frontend
// calls it at the start of each turn.
func (e *Elicitor) SetAsker(a tools.Asker) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ask = a
}

func (e *Elicitor) asker() tools.Asker {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.ask
}

// handlerFor returns the SDK elicitation handler for one server. The server
// name is bound here rather than read from the request because the request does
// not carry it, and "a server wants your GitHub token" is a question nobody can
// answer safely without knowing which server.
func (e *Elicitor) handlerFor(server string) func(context.Context, *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error) {
	if e == nil {
		return nil
	}
	return func(ctx context.Context, req *mcpsdk.ElicitRequest) (*mcpsdk.ElicitResult, error) {
		return e.elicit(ctx, server, req.Params)
	}
}

// Sentinel option labels. The user can always answer in their own words, so
// these are matched leniently (see matchesLabel) — someone who types "cancel"
// at a prompt meant the Cancel option, not a value of "cancel".
const (
	optCancel  = "Cancel"
	optSkip    = "Skip"
	optDefault = "Use default"
	optYes     = "Yes"
	optNo      = "No"
)

const (
	actionAccept  = "accept"
	actionDecline = "decline"
	actionCancel  = "cancel"
)

func (e *Elicitor) elicit(ctx context.Context, server string, p *mcpsdk.ElicitParams) (*mcpsdk.ElicitResult, error) {
	if p == nil {
		return &mcpsdk.ElicitResult{Action: actionCancel}, nil
	}
	ask := e.asker()
	if ask == nil {
		// Advertised the capability (an Elicitor exists) but no turn is in
		// flight to carry the question. Decline rather than cancel: there is no
		// user, so re-asking will not help either.
		return &mcpsdk.ElicitResult{Action: actionDecline}, nil
	}
	// A url-mode request from a server that ignored our form-only capability.
	if strings.EqualFold(strings.TrimSpace(p.Mode), "url") || p.URL != "" {
		return &mcpsdk.ElicitResult{Action: actionDecline}, nil
	}

	fields, err := elicitFields(p.RequestedSchema)
	if err != nil {
		// A schema we cannot read is the server's bug, not the user's decision
		// to make; do not show them a prompt built from it.
		return &mcpsdk.ElicitResult{Action: actionDecline}, nil
	}

	header := elicitHeader(server, p.Message)

	e.askMu.Lock()
	defer e.askMu.Unlock()

	// No fields is a plain confirmation, and the only form elicitation where
	// "decline" is a meaningful distinct answer: the user said no to the thing
	// itself rather than failing to supply a value for it.
	if len(fields) == 0 {
		answer, err := ask.Ask(ctx, header, []tools.AskOption{
			{Label: optYes},
			{Label: optNo},
		})
		if err != nil {
			return nil, err
		}
		if b, ok := coerceBool(answer); ok && b {
			return &mcpsdk.ElicitResult{Action: actionAccept, Content: map[string]any{}}, nil
		}
		return &mcpsdk.ElicitResult{Action: actionDecline}, nil
	}

	content := map[string]any{}
	for _, f := range fields {
		answer, err := ask.Ask(ctx, header+"\n\n"+f.prompt(), f.options())
		if err != nil {
			return nil, err
		}
		switch {
		case matchesLabel(answer, f.label(optCancel)):
			return &mcpsdk.ElicitResult{Action: actionCancel}, nil
		case !f.required && matchesLabel(answer, f.label(optSkip)):
			continue
		case f.hasDef && matchesLabel(answer, f.label(optDefault)):
			content[f.key] = f.def
			continue
		}
		v, ok := f.coerce(answer)
		if !ok {
			return &mcpsdk.ElicitResult{Action: actionCancel}, nil
		}
		content[f.key] = v
	}
	return &mcpsdk.ElicitResult{Action: actionAccept, Content: content}, nil
}

// elicitHeader names the server in the question. Without it the prompt is a
// bare sentence from a third party with nothing to attribute it to.
func elicitHeader(server, message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "needs some input."
	}
	return fmt.Sprintf("MCP server %q: %s", server, message)
}

// elicitField is one top-level property of a requested schema. Nesting is not
// allowed by the spec, so there is no recursion here.
//
// A multi-select field (an array of enum strings) keeps its item type in typ
// and sets multi; the answer is then a comma-separated list of choices.
type elicitField struct {
	key       string
	title     string
	desc      string
	typ       string
	enum      []string
	enumNames []string
	required  bool
	multi     bool
	minItems  int
	maxItems  int // 0 is unbounded
	def       any
	hasDef    bool
}

// enumSchema is the part of a property that can list choices: "enum" (with
// the legacy "enumNames" for labels), or "oneOf"/"anyOf" of {const, title},
// which is how the spec spells a titled enum.
type enumSchema struct {
	Type      string   `json:"type"`
	Enum      []any    `json:"enum"`
	EnumNames []string `json:"enumNames"`
	OneOf     []choice `json:"oneOf"`
	AnyOf     []choice `json:"anyOf"`
}

type choice struct {
	Const any    `json:"const"`
	Title string `json:"title"`
}

// choices returns the enum members and their display names.
func (e enumSchema) choices() (values []string, names []string) {
	for _, v := range e.Enum {
		values = append(values, enumLabel(v))
	}
	names = e.EnumNames
	if len(values) > 0 {
		return values, names
	}
	titled := e.OneOf
	if len(titled) == 0 {
		titled = e.AnyOf
	}
	names = nil
	for _, c := range titled {
		values = append(values, enumLabel(c.Const))
		names = append(names, c.Title)
	}
	return values, names
}

// schemaDoc is the subset of JSON Schema that form elicitation permits.
type schemaDoc struct {
	Properties map[string]struct {
		enumSchema
		Title       string          `json:"title"`
		Description string          `json:"description"`
		Default     json.RawMessage `json:"default"`
		Items       *enumSchema     `json:"items"`
		MinItems    int             `json:"minItems"`
		MaxItems    int             `json:"maxItems"`
	} `json:"properties"`
	Required []string `json:"required"`
}

// elicitFields flattens a requested schema into the order the fields will be
// asked in: required ones first, in the order the schema's "required" array
// lists them, then the rest sorted by key.
//
// Declaration order is not available for the optional ones. The schema reaches
// us as a decoded map (the SDK types RequestedSchema as `any`), and JSON object
// key order does not survive decoding. "required" is an array, so that part of
// the order is real and worth honouring — it is also the part the user cares
// about, since those are the questions they cannot skip.
func elicitFields(raw any) ([]elicitField, error) {
	if raw == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var doc schemaDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	required := map[string]bool{}
	for _, k := range doc.Required {
		required[k] = true
	}
	build := func(key string) (elicitField, bool) {
		p, ok := doc.Properties[key]
		if !ok {
			return elicitField{}, false
		}
		f := elicitField{
			key:      key,
			title:    p.Title,
			desc:     p.Description,
			typ:      strings.ToLower(strings.TrimSpace(p.Type)),
			required: required[key],
		}
		f.enum, f.enumNames = p.choices()
		if f.typ == "array" && p.Items != nil {
			f.multi = true
			f.typ = strings.ToLower(strings.TrimSpace(p.Items.Type))
			f.enum, f.enumNames = p.Items.choices()
			f.minItems, f.maxItems = p.MinItems, p.MaxItems
		}
		if len(p.Default) > 0 && string(p.Default) != "null" {
			f.hasDef = json.Unmarshal(p.Default, &f.def) == nil
		}
		return f, true
	}
	var out []elicitField
	seen := map[string]bool{}
	for _, k := range doc.Required {
		if seen[k] {
			continue
		}
		seen[k] = true
		if f, ok := build(k); ok {
			out = append(out, f)
		}
	}
	rest := make([]string, 0, len(doc.Properties))
	for k := range doc.Properties {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		if f, ok := build(k); ok {
			out = append(out, f)
		}
	}
	return out, nil
}

// enumLabel renders an enum member for display. Numbers are formatted without
// the float noise encoding/json leaves on them ("3" rather than "3e+00").
func enumLabel(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case nil:
		return ""
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(b)
	}
}

// prompt is the per-field question text.
func (f elicitField) prompt() string {
	label := f.title
	if strings.TrimSpace(label) == "" {
		label = f.key
	}
	var b strings.Builder
	b.WriteString(label)
	if f.title != "" && f.title != f.key {
		// Both, because the server gets back a map keyed by the key and the
		// title alone can be ambiguous between two similar fields.
		fmt.Fprintf(&b, " (%s)", f.key)
	}
	if !f.required {
		b.WriteString(" — optional")
	}
	if f.multi {
		b.WriteString(" — pick one or more, separated by commas")
	}
	if f.hasDef {
		fmt.Fprintf(&b, "\nDefault: %s", f.defLabel())
	}
	if d := strings.TrimSpace(f.desc); d != "" {
		b.WriteString("\n")
		b.WriteString(d)
	}
	return b.String()
}

// options lists the choices offered for this field. A free-text field offers
// only the escape hatches; the value itself is typed.
func (f elicitField) options() []tools.AskOption {
	var out []tools.AskOption
	switch {
	case len(f.enum) > 0:
		for i, v := range f.enum {
			opt := tools.AskOption{Label: v}
			// enumNames is the display form; keep the wire value visible as
			// the description so the user can see what is actually sent.
			if i < len(f.enumNames) && f.enumNames[i] != "" && f.enumNames[i] != v {
				opt = tools.AskOption{Label: f.enumNames[i], Description: v}
			}
			out = append(out, opt)
		}
	case f.typ == "boolean":
		out = append(out, tools.AskOption{Label: optYes}, tools.AskOption{Label: optNo})
	}
	// A single choice's default is already a row of its own; a typed value or
	// a set of choices needs one to accept it without retyping.
	if f.hasDef && (f.multi || (len(f.enum) == 0 && f.typ != "boolean")) {
		out = append(out, tools.AskOption{Label: f.label(optDefault), Description: f.defLabel()})
	}
	if !f.required {
		out = append(out, tools.AskOption{Label: f.label(optSkip), Description: "leave this field unset"})
	}
	return append(out, tools.AskOption{Label: f.label(optCancel), Description: "answer nothing and tell the server you cancelled"})
}

// label returns a sentinel option's label for this field, reworded when the
// field offers a choice of the same name — a server asking "Skip or Retry?"
// must get "Skip" back, not have its field silently left unset.
func (f elicitField) label(sentinel string) string {
	for i, v := range f.enum {
		if strings.EqualFold(v, sentinel) || (i < len(f.enumNames) && strings.EqualFold(f.enumNames[i], sentinel)) {
			return map[string]string{
				optCancel:  "Cancel the form",
				optSkip:    "Skip this field",
				optDefault: "Use the default",
			}[sentinel]
		}
	}
	return sentinel
}

// defLabel renders the default for display: its enum name when it has one.
func (f elicitField) defLabel() string {
	vals, ok := f.def.([]any)
	if !ok {
		vals = []any{f.def}
	}
	parts := make([]string, 0, len(vals))
	for _, v := range vals {
		l := enumLabel(v)
		for i, e := range f.enum {
			if e == l && i < len(f.enumNames) && f.enumNames[i] != "" {
				l = f.enumNames[i]
			}
		}
		parts = append(parts, l)
	}
	return strings.Join(parts, ", ")
}

// coerce turns the user's answer into a value of the field's declared type.
// The second result is false when it cannot, which cancels the elicitation.
func (f elicitField) coerce(answer string) (any, bool) {
	answer = strings.TrimSpace(answer)
	if f.multi {
		return f.coerceMulti(answer)
	}
	// An enum answered with a display name has to be mapped back to the wire
	// value, or the server is handed a label it never offered.
	if len(f.enum) > 0 {
		for i, v := range f.enum {
			if strings.EqualFold(answer, v) {
				return f.enumValue(i), true
			}
			if i < len(f.enumNames) && f.enumNames[i] != "" && strings.EqualFold(answer, f.enumNames[i]) {
				return f.enumValue(i), true
			}
		}
		// Not one of the offered values. The schema says it must be, so this
		// is not a value the server will accept.
		return nil, false
	}
	switch f.typ {
	case "boolean":
		b, ok := coerceBool(answer)
		return b, ok
	case "integer":
		if n, err := strconv.ParseInt(answer, 10, 64); err == nil {
			return n, true
		}
		// "3.0" is an integer; "3.5" is not.
		if x, err := strconv.ParseFloat(answer, 64); err == nil && x == math.Trunc(x) && !math.IsInf(x, 0) {
			return int64(x), true
		}
		return nil, false
	case "number":
		x, err := strconv.ParseFloat(answer, 64)
		if err != nil || math.IsInf(x, 0) || math.IsNaN(x) {
			return nil, false
		}
		return x, true
	default: // "string", and anything the spec does not define
		if answer == "" && f.required {
			return nil, false
		}
		return answer, true
	}
}

// coerceMulti reads a comma-separated multi-select answer. Every item must be
// one of the choices, and the count must sit within minItems/maxItems — the
// server's schema says anything else will be rejected.
func (f elicitField) coerceMulti(answer string) (any, bool) {
	out := []any{}
	seen := map[string]bool{}
	for _, part := range strings.Split(answer, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		item := f
		item.multi = false
		v, ok := item.coerce(part)
		if !ok {
			return nil, false
		}
		if k := enumLabel(v); !seen[k] {
			seen[k] = true
			out = append(out, v)
		}
	}
	if len(out) < f.minItems || (f.maxItems > 0 && len(out) > f.maxItems) {
		return nil, false
	}
	if len(out) == 0 && f.required && f.minItems == 0 {
		return nil, false
	}
	return out, true
}

// enumValue returns the i-th enum member in its declared type. Booleans and
// numbers are rendered as strings for display and have to go back as what the
// schema said they were.
func (f elicitField) enumValue(i int) any {
	v := f.enum[i]
	switch f.typ {
	case "boolean":
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	case "integer":
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	case "number":
		if x, err := strconv.ParseFloat(v, 64); err == nil {
			return x
		}
	}
	return v
}

// coerceBool reads a yes/no answer. The user is offered buttons but may type,
// so the affirmative and negative words both have to be understood.
func coerceBool(answer string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes", "true", "1", "on", "ok", "okay":
		return true, true
	case "n", "no", "false", "0", "off":
		return false, true
	}
	return false, false
}

// matchesLabel reports whether an answer selected the given sentinel option.
// The TUI returns the label verbatim when the user picks it by number, so the
// exact match carries the common case; the lenient match catches the user who
// typed the word instead.
func matchesLabel(answer, label string) bool {
	return strings.EqualFold(strings.TrimSpace(answer), label)
}
