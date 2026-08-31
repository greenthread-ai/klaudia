package mcp

// stripJSONComments removes // line and /* block */ comments from JSON,
// preserving byte offsets by replacing comment bytes with spaces so that any
// parse error still points at the right place.
//
// .mcp.json is a config file people edit by hand, and Klaudia's own
// documentation has always shown it with comments — which encoding/json
// rejects. Accepting them here is cheaper than an errata, and comments in a
// wiring file ("this server is the staging one") are worth having.
//
// Only comments outside strings are removed: a URL like "https://example.com"
// keeps its slashes, which is the case a naive strip gets wrong.
func stripJSONComments(data []byte) []byte {
	out := make([]byte, len(data))
	copy(out, data)

	const (
		text = iota
		inString
		inLine
		inBlock
	)
	state := text
	for i := 0; i < len(out); i++ {
		c := out[i]
		switch state {
		case text:
			switch {
			case c == '"':
				state = inString
			case c == '/' && i+1 < len(out) && out[i+1] == '/':
				state = inLine
				out[i], out[i+1] = ' ', ' '
				i++
			case c == '/' && i+1 < len(out) && out[i+1] == '*':
				state = inBlock
				out[i], out[i+1] = ' ', ' '
				i++
			}
		case inString:
			if c == '\\' {
				i++ // escaped byte is never a quote
				continue
			}
			if c == '"' {
				state = text
			}
		case inLine:
			if c == '\n' {
				state = text
				continue // keep the newline: line numbers must not move
			}
			out[i] = ' '
		case inBlock:
			if c == '*' && i+1 < len(out) && out[i+1] == '/' {
				out[i], out[i+1] = ' ', ' '
				i++
				state = text
				continue
			}
			if c != '\n' { // keep newlines for line numbers
				out[i] = ' '
			}
		}
	}
	return out
}
