package tools

import "fmt"

// DefaultRegistry builds the registry of all implemented local tools. New tools
// are added here as they are ported. Each constructor may fail if its input
// schema can't be generated.
func DefaultRegistry() (*Registry, error) {
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
