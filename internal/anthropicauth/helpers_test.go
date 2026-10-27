package anthropicauth

import "github.com/greenthread-ai/klaudia/pkg/api"

var zeroRetries = 0

// noRetries keeps a test against an erroring server to a single request.
var noRetries = api.Tuning{MaxRetries: &zeroRetries}
