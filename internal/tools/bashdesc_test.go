package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBashDescriptionExplainsShellAndJobs(t *testing.T) {
	b, err := NewBash(nil)
	if err != nil {
		t.Fatal(err)
	}
	desc, _ := b.Description(context.Background())
	for _, want := range []string{"new shell", "`cd`", "do not carry over", "run_in_background", "BashOutput", "KillShell"} {
		if !strings.Contains(desc, want) {
			t.Errorf("Bash description does not mention %q:\n%s", want, desc)
		}
	}
}

func TestTimeoutNoteNamesLimitAndWayOut(t *testing.T) {
	short := timeoutNote(2 * time.Minute)
	if !strings.Contains(short, "2m0s") || !strings.Contains(short, "600000") || !strings.Contains(short, "run_in_background") {
		t.Errorf("default-limit note should name the limit, the max and the job route: %q", short)
	}
	max := timeoutNote(10 * time.Minute)
	if !strings.Contains(max, "maximum") || strings.Contains(max, "larger timeout") {
		t.Errorf("at the maximum, raising the timeout is not an option: %q", max)
	}
}
