package tasks

import "testing"

func TestStoreCreateGetList(t *testing.T) {
	store := New()

	first := store.Create("Write code", "Implement task store", "Writing code")
	second := store.Create("Run tests", "Verify behavior", "Running tests")

	if first.ID != "task-1" || second.ID != "task-2" {
		t.Fatalf("ids = %q, %q; want task-1, task-2", first.ID, second.ID)
	}
	if first.Status != "pending" {
		t.Fatalf("status = %q, want pending", first.Status)
	}
	got, ok := store.Get("task-1")
	if !ok {
		t.Fatal("Get(task-1) ok = false")
	}
	if got.Subject != "Write code" || got.Description != "Implement task store" || got.ActiveForm != "Writing code" {
		t.Fatalf("Get(task-1) = %+v", got)
	}

	list := store.List()
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}
	if list[0].ID != "task-1" || list[1].ID != "task-2" {
		t.Fatalf("List order = %+v", list)
	}
	list[0].Subject = "mutated"
	got, _ = store.Get("task-1")
	if got.Subject == "mutated" {
		t.Fatal("List returned mutable backing storage")
	}
}

func TestStoreUpdate(t *testing.T) {
	store := New()
	created := store.Create("Write code", "Implement task store", "Writing code")

	updated, err := store.Update(created.ID, "in_progress", "Write tests", "Cover task store", "Writing tests")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Status != "in_progress" || updated.Subject != "Write tests" || updated.Description != "Cover task store" || updated.ActiveForm != "Writing tests" {
		t.Fatalf("updated = %+v", updated)
	}

	unchanged, err := store.Update(created.ID, "completed", "", "", "")
	if err != nil {
		t.Fatalf("Update status only: %v", err)
	}
	if unchanged.Status != "completed" || unchanged.Subject != "Write tests" || unchanged.Description != "Cover task store" || unchanged.ActiveForm != "Writing tests" {
		t.Fatalf("empty fields should leave values unchanged: %+v", unchanged)
	}
}

func TestStoreUpdateUnknownID(t *testing.T) {
	store := New()

	if _, err := store.Update("task-404", "completed", "", "", ""); err == nil {
		t.Fatal("expected unknown id error")
	}
}
