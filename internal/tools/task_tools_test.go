package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/greenthread/klaudia/internal/tasks"
)

func TestTaskToolsHappyPath(t *testing.T) {
	store := tasks.New()
	create, err := NewTaskCreate(store)
	if err != nil {
		t.Fatal(err)
	}
	list, err := NewTaskList(store)
	if err != nil {
		t.Fatal(err)
	}
	get, err := NewTaskGet(store)
	if err != nil {
		t.Fatal(err)
	}
	update, err := NewTaskUpdate(store)
	if err != nil {
		t.Fatal(err)
	}

	createRaw, _ := json.Marshal(TaskCreateInput{Subject: "Write code", Description: "Implement task tools", ActiveForm: "Writing code"})
	res, err := create.Execute(context.Background(), Context{}, createRaw)
	if err != nil || res[0].IsError {
		t.Fatalf("create: err=%v res=%+v", err, res[0])
	}
	if res[0].Content != "Created task-1: Write code" {
		t.Fatalf("create content = %q", res[0].Content)
	}

	listRaw, _ := json.Marshal(TaskListInput{})
	res, err = list.Execute(context.Background(), Context{}, listRaw)
	if err != nil || res[0].IsError {
		t.Fatalf("list: err=%v res=%+v", err, res[0])
	}
	if res[0].Content != "task-1 [pending] Write code" {
		t.Fatalf("list content = %q", res[0].Content)
	}

	getRaw, _ := json.Marshal(TaskGetInput{ID: "task-1"})
	res, err = get.Execute(context.Background(), Context{}, getRaw)
	if err != nil || res[0].IsError {
		t.Fatalf("get: err=%v res=%+v", err, res[0])
	}
	for _, want := range []string{"ID: task-1", "Subject: Write code", "Description: Implement task tools", "Status: pending", "ActiveForm: Writing code"} {
		if !strings.Contains(res[0].Content, want) {
			t.Fatalf("get content %q missing %q", res[0].Content, want)
		}
	}

	updateRaw, _ := json.Marshal(TaskUpdateInput{ID: "task-1", Status: "in_progress", Subject: "Run tests", Description: "Verify task tools", ActiveForm: "Running tests"})
	res, err = update.Execute(context.Background(), Context{}, updateRaw)
	if err != nil || res[0].IsError {
		t.Fatalf("update: err=%v res=%+v", err, res[0])
	}
	if res[0].Content != "Updated task-1: Run tests" {
		t.Fatalf("update content = %q", res[0].Content)
	}

	res, err = list.Execute(context.Background(), Context{}, listRaw)
	if err != nil || res[0].IsError {
		t.Fatalf("list after update: err=%v res=%+v", err, res[0])
	}
	if res[0].Content != "task-1 [in_progress] Run tests" {
		t.Fatalf("updated list content = %q", res[0].Content)
	}
}

func TestTaskToolsEmptyListAndErrors(t *testing.T) {
	store := tasks.New()
	list, _ := NewTaskList(store)
	get, _ := NewTaskGet(store)
	update, _ := NewTaskUpdate(store)

	listRaw, _ := json.Marshal(TaskListInput{})
	res, err := list.Execute(context.Background(), Context{}, listRaw)
	if err != nil || res[0].IsError {
		t.Fatalf("empty list: err=%v res=%+v", err, res[0])
	}
	if res[0].Content != "No tasks." {
		t.Fatalf("empty list content = %q", res[0].Content)
	}

	getRaw, _ := json.Marshal(TaskGetInput{ID: "task-404"})
	res, err = get.Execute(context.Background(), Context{}, getRaw)
	if err != nil {
		t.Fatalf("get not found: %v", err)
	}
	if !res[0].IsError || res[0].Content != "Task task-404 not found" {
		t.Fatalf("get not found res = %+v", res[0])
	}

	updateRaw, _ := json.Marshal(TaskUpdateInput{ID: "task-404", Status: "completed"})
	res, err = update.Execute(context.Background(), Context{}, updateRaw)
	if err != nil {
		t.Fatalf("update not found: %v", err)
	}
	if !res[0].IsError || !strings.Contains(res[0].Content, "task-404") {
		t.Fatalf("update not found res = %+v", res[0])
	}

	invalidRaw, _ := json.Marshal(TaskUpdateInput{ID: "task-1", Status: "bogus"})
	if err := update.ValidateInput(invalidRaw); err == nil {
		t.Fatal("expected invalid status error")
	}
}
