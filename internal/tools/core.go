package tools

import "github.com/greenthread-ai/klaudia/pkg/tool"

// The tool contract lives in pkg/tool, where a product embedding the agent
// loop can implement it. These aliases keep Klaudia's built-in tools — and
// everything that names tools.Context or tools.Result — written as they were.

type (
	Tool              = tool.Tool
	Context           = tool.Context
	Result            = tool.Result
	ResultImage       = tool.ResultImage
	Registry          = tool.Registry
	AskOption         = tool.AskOption
	Asker             = tool.Asker
	Planner           = tool.Planner
	BackgroundRunner  = tool.BackgroundRunner
	ConcurrencySafe   = tool.ConcurrencySafe
	SpillStore        = tool.SpillStore
	HostApprover      = tool.HostApprover
	HostChangeRequest = tool.HostChangeRequest
	HostChangeOutcome = tool.HostChangeOutcome
)

// NewRegistry and IsConcurrencySafe are tool's.
var (
	NewRegistry       = tool.NewRegistry
	IsConcurrencySafe = tool.IsConcurrencySafe
)
