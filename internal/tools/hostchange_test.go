package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeHostApprover struct {
	out HostChangeOutcome
	err error
	got []HostChangeRequest
}

func (f *fakeHostApprover) RequestHostChange(_ context.Context, req HostChangeRequest) (HostChangeOutcome, error) {
	f.got = append(f.got, req)
	return f.out, f.err
}

func newHostChange(t *testing.T) *RequestHostChange {
	t.Helper()
	r, err := NewRequestHostChange()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestHostChangeValidateRequiresAScopedDeclaration(t *testing.T) {
	r := newHostChange(t)
	cases := []struct {
		name    string
		in      HostChangeInput
		wantErr string // "" means valid
	}{
		{"complete", HostChangeInput{Summary: "Install nginx", Reason: "proxy", Packages: []string{"nginx"}}, ""},
		{"paths alone are a scope", HostChangeInput{Summary: "s", Reason: "r", Paths: []string{"/etc/hosts"}}, ""},
		{"services alone are a scope", HostChangeInput{Summary: "s", Reason: "r", Services: []string{"nginx"}}, ""},
		{"blank summary", HostChangeInput{Summary: "  ", Reason: "r", Packages: []string{"x"}}, "summary is required"},
		{"blank reason", HostChangeInput{Summary: "s", Reason: "", Packages: []string{"x"}}, "reason is required"},
		{"no scope", HostChangeInput{Summary: "s", Reason: "r"}, "declare at least one"},
		{"glob path", HostChangeInput{Summary: "s", Reason: "r", Paths: []string{"/etc/nginx/*.conf"}}, "is a pattern"},
		{"bracket path", HostChangeInput{Summary: "s", Reason: "r", Paths: []string{"/etc/[ab]"}}, "is a pattern"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, _ := json.Marshal(c.in)
			err := r.ValidateInput(raw)
			switch {
			case c.wantErr == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
				t.Errorf("err = %v, want one containing %q", err, c.wantErr)
			}
		})
	}
}

func TestHostChangeWithoutApproverRefusesPlainly(t *testing.T) {
	r := newHostChange(t)
	res := runTool(t, r, Context{}, HostChangeInput{Summary: "s", Reason: "r", Packages: []string{"x"}})
	if !res.IsError || !strings.Contains(res.Content, "no one to ask") {
		t.Errorf("res = %+v, want an error saying there is no approver", res)
	}
}

func TestHostChangeOutcomes(t *testing.T) {
	in := HostChangeInput{
		Summary:  "  Install nginx  ",
		Reason:   `needs a \"proxy\"`,
		Paths:    []string{"/etc/nginx/nginx.conf"},
		Services: []string{"nginx"},
		Packages: []string{"nginx"},
	}
	cases := []struct {
		name      string
		approver  *fakeHostApprover
		wantErr   bool
		wantStart string
	}{
		{"approved", &fakeHostApprover{out: HostChangeOutcome{Approved: true}}, false, "Approved: Install nginx."},
		{"declined", &fakeHostApprover{out: HostChangeOutcome{}}, false, "The user declined"},
		{"custom refusal", &fakeHostApprover{out: HostChangeOutcome{Message: "not on my laptop"}}, true, "not on my laptop"},
		{"custom approval", &fakeHostApprover{out: HostChangeOutcome{Approved: true, Message: "ok, go"}}, false, "ok, go"},
		{"approver fails", &fakeHostApprover{err: errors.New("tty gone")}, true, "Could not ask the user: tty gone"},
	}
	r := newHostChange(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := runTool(t, r, Context{HostChange: c.approver}, in)
			if res.IsError != c.wantErr {
				t.Errorf("IsError = %v, want %v (%q)", res.IsError, c.wantErr, res.Content)
			}
			if !strings.HasPrefix(res.Content, c.wantStart) {
				t.Errorf("content = %q, want prefix %q", res.Content, c.wantStart)
			}
			if len(c.approver.got) != 1 {
				t.Fatalf("approver asked %d times, want 1", len(c.approver.got))
			}
			// What the user sees is trimmed and un-double-escaped; the scope is
			// passed through untouched.
			got := c.approver.got[0]
			if got.Summary != "Install nginx" || got.Reason != `needs a "proxy"` {
				t.Errorf("request text = %q / %q", got.Summary, got.Reason)
			}
			if len(got.Paths) != 1 || len(got.Services) != 1 || len(got.Packages) != 1 {
				t.Errorf("scope not passed through: %+v", got)
			}
		})
	}
}
