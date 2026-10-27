package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

func TestJobsPanelListsRunningWork(t *testing.T) {
	m := newTestModel()
	m.resize(100, 30)
	now := time.Now()
	m.sess.Jobs = &fakeJobs{jobs: []tools.JobStatus{
		{Name: "web", Command: "npm run dev", Running: true, Port: "3000", Started: now.Add(-2 * time.Minute)},
		{Name: "done", Command: "make", Running: false, Started: now.Add(-time.Hour)},
		{Name: "db", Command: "postgres -D data", Running: true, Started: now.Add(-5 * time.Minute)},
	}}
	view := stripANSI(m.bottomView())
	web, db := strings.Index(view, "⏵ web"), strings.Index(view, "⏵ db")
	if web < 0 || db < 0 || strings.Contains(view, "⏵ done") {
		t.Fatalf("want the two running jobs only:\n%s", view)
	}
	if db > web {
		t.Errorf("oldest job should be first:\n%s", view)
	}
	if !strings.Contains(view, ":3000") || !strings.Contains(view, "npm run dev") {
		t.Errorf("port or command missing:\n%s", view)
	}
	// Under the prompt, above the status line.
	if box, status := strings.Index(view, "╰"), strings.LastIndex(view, "\n"); !(box < web && web < status) {
		t.Errorf("panel should sit between the input box and the status line:\n%s", view)
	}
	// A tick is armed while jobs run, once.
	if m.jobsTick() == nil || m.jobsTick() != nil {
		t.Error("want exactly one tick scheduled while jobs run")
	}
}

func TestJobsPanelOverflowAndIdle(t *testing.T) {
	m := newTestModel()
	m.resize(100, 30)
	var jobs []tools.JobStatus
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		jobs = append(jobs, tools.JobStatus{Name: n, Running: true, Started: time.Now()})
	}
	m.sess.Jobs = &fakeJobs{jobs: jobs}
	if view := stripANSI(m.bottomView()); !strings.Contains(view, "+2 more · /jobs") {
		t.Errorf("overflow not summarised:\n%s", view)
	}
	m.sess.Jobs = &fakeJobs{}
	if m.jobsPanel() != "" || m.jobsTick() != nil {
		t.Error("no jobs: no panel and no tick")
	}
}
