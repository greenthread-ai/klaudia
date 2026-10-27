package api

// Credential is the resolved auth used to talk to the Anthropic API.
//
// Exactly one of APIKey / AuthToken is set. APIKey maps to the `x-api-key`
// header; AuthToken is an OAuth bearer token (Authorization: Bearer …).
type Credential struct {
	APIKey    string
	AuthToken string
}

// IsOAuth reports whether this credential is an OAuth bearer token.
func (c Credential) IsOAuth() bool { return c.AuthToken != "" }
