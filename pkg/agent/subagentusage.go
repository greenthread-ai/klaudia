package agent

import "context"

// A running sub-agent's token count, reported while it runs.
//
// The Agent tool's progress callback carries lines; the tokens a child has
// spent arrive separately, through a sink the parent puts in the context it
// hands the tool. The dispatcher's sink turns each update into a
// tool_progress event with Tokens set; a background run's sink stores it on
// the task. The spawner feeds it from the child's own usage events, so the
// figure moves after every model call, not only when the child calls a
// tool.

type usageSinkKey struct{}

// withUsageSink returns ctx carrying fn, which receives a sub-agent's total
// tokens so far (prompt, cache included, plus output).
func withUsageSink(ctx context.Context, fn func(total int64)) context.Context {
	return context.WithValue(ctx, usageSinkKey{}, fn)
}

// usageSinkFrom returns the sink in ctx, or nil.
func usageSinkFrom(ctx context.Context) func(int64) {
	fn, _ := ctx.Value(usageSinkKey{}).(func(int64))
	return fn
}
