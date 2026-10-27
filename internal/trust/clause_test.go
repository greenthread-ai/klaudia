package trust

import "testing"

// The plan-mode clause (agent/mode.go) and the Plan sub-agent's prompt tell
// the model these commands are available in plan mode. If the classifier
// stops reading any of them as read-only, the prompt is promising something
// the gate will refuse or ask about — change both together.
func TestCommandsThePlanPromptsPromiseAreReadOnly(t *testing.T) {
	for _, c := range []string{"git log", "git diff", "git status", "ls -la", "sed -n 1,5p f", "go doc fmt", "go list ./..."} {
		if m, why := InspectCommand(c); m != MutationNone {
			t.Errorf("%q is %v (%s); the plan-mode prompts name it as available", c, m, why)
		}
	}
}
