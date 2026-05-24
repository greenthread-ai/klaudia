package tools

import (
	"fmt"

	"github.com/greenthread/klaudia/internal/sandbox"
)

// DefaultRegistry builds the registry of all implemented local tools. New tools
// are added here as they are ported. Each constructor may fail if its input
// schema can't be generated.
func DefaultRegistry() (*Registry, error) {
	// The Bash tool runs through an Executor. Phase 2 uses the unconfined
	// local host executor; sandbox/container executors swap in later.
	executor := sandbox.NewLocal()
	// Per-session todo state shared with the TodoWrite tool.
	todos := &TodoStore{}

	type ctor struct {
		name string
		make func() (Tool, error)
	}
	ctors := []ctor{
		{"Read", func() (Tool, error) { return NewRead() }},
		{"Write", func() (Tool, error) { return NewWrite() }},
		{"Edit", func() (Tool, error) { return NewEdit() }},
		{"Glob", func() (Tool, error) { return NewGlob() }},
		{"Grep", func() (Tool, error) { return NewGrep() }},
		{"Bash", func() (Tool, error) { return NewBash(executor) }},
		{"TodoWrite", func() (Tool, error) { return NewTodoWrite(todos) }},
		{"NotebookEdit", func() (Tool, error) { return NewNotebookEdit() }},
	}

	ts := make([]Tool, 0, len(ctors))
	for _, c := range ctors {
		t, err := c.make()
		if err != nil {
			return nil, fmt.Errorf("build tool %s: %w", c.name, err)
		}
		ts = append(ts, t)
	}
	return NewRegistry(ts...), nil
}
