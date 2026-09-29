package tools

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/greenthread-ai/klaudia/internal/sandbox"
)

// A read used to return everything since the previous one: a chatty job left
// alone for a few turns could put megabytes into the context in one call.
func TestBashOutputClampsALongRead(t *testing.T) {
	store := newTestJobStore(t)
	// ~140 KB of numbered lines, well over the cap.
	res, err := store.Start(sandbox.NewLocal(), sandbox.Request{
		Command: "i=0; while [ $i -lt 4000 ]; do printf 'line %05d of the chatty dev server\\n' $i; i=$((i+1)); done",
	})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Job.ID
	if !waitUntil(20*time.Second, func() bool { return !peekRunning(store, id) }) {
		t.Fatal("job did not exit in time")
	}

	tool, _ := NewBashOutput(store)
	out, err := tool.Execute(context.Background(), Context{}, []byte(`{"bash_id":"`+id+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	r := out[0]
	if len(r.Content) > bashMaxOutput+500 {
		t.Errorf("read is %d bytes in context, want it capped near %d", len(r.Content), bashMaxOutput)
	}
	for _, want := range []string{"line 00000 ", "line 03999 ", "bytes elided", "[full log: ", "exited, code 0"} {
		if !strings.Contains(r.Content, want) {
			t.Errorf("capped read is missing %q", want)
		}
	}
	// Tail-biased: ~15 KB from the end survives, which Bash's head-heavy 10 KB
	// tail would have cut. Each line is 36 bytes, so that is line 3999-430.
	if !strings.Contains(r.Content, "line 03569 ") {
		t.Error("the recent output should get the larger share of the budget")
	}
	var path string
	for _, j := range store.List() {
		if j.ID == id {
			path = j.LogPath
		}
	}
	if path == "" || !strings.Contains(r.Content, path) {
		t.Errorf("notice should name the job log %q", path)
	}
	if !strings.Contains(r.Full, "line 02000 ") || !strings.HasSuffix(r.Full, "exited, code 0]") {
		t.Error("the untrimmed read should be kept for local display, with its status")
	}
}

func TestClampJobRead(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	if model, full := clampJobRead("short\n", ShellOutput{}); model != "short\n" || full != "" {
		t.Errorf("short read changed: %q / %q", model, full)
	}
	if model, _ := clampJobRead("", ShellOutput{}); model != "[no new output]" {
		t.Errorf("empty read = %q", model)
	}

	// A memory-only log has no file to point at, so the read is spilled.
	body := strings.Repeat("memory-only job output\n", 3000)
	model, full := clampJobRead(body, ShellOutput{})
	if full != body {
		t.Error("full should be the untrimmed read")
	}
	i := strings.Index(model, spillMarker)
	if i < 0 {
		t.Fatalf("no spill notice at the end of %q", model[len(model)-200:])
	}
	path := strings.TrimSuffix(model[i+len(spillMarker):], "]")
	if data, err := os.ReadFile(path); err != nil || string(data) != body {
		t.Errorf("spill file %q does not hold the read (err %v)", path, err)
	}
}
