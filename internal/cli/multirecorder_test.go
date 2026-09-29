package cli

import (
	"encoding/json"
	"errors"
	"testing"
)

type countRecorder struct {
	n   int
	err error
}

func (c *countRecorder) Record(string, json.RawMessage) error { c.n++; return c.err }

// A failing transcript must not stop the stream-json envelope behind it.
func TestMultiRecorderWritesAllAndReportsFirstError(t *testing.T) {
	failing := &countRecorder{err: errors.New("disk full")}
	envelope := &countRecorder{}
	err := multiRecorder{failing, nil, envelope}.Record("user", json.RawMessage(`{}`))
	if err == nil || err.Error() != "disk full" {
		t.Errorf("err = %v, want the first failure", err)
	}
	if envelope.n != 1 {
		t.Errorf("envelope recorder called %d times after the transcript failed, want 1", envelope.n)
	}
}
