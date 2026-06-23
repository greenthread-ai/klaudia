// Package tasks holds the in-memory task store backing the TaskCreate, TaskList,
// TaskGet, and TaskUpdate tools — a lightweight, session-scoped to-do list the
// agent uses to track multi-step work.
package tasks

import (
	"fmt"
	"sync"
)

// Task is one tracked task in a session.
type Task struct {
	ID          string
	Subject     string
	Description string
	Status      string
	ActiveForm  string
}

// Store holds tasks in creation order. It is safe for concurrent use.
type Store struct {
	mu     sync.Mutex
	nextID int
	items  []Task
}

// New constructs an empty task store.
func New() *Store {
	return &Store{nextID: 1}
}

// Create adds a new pending task and returns it.
func (s *Store) Create(subject, description, activeForm string) Task {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.nextID == 0 {
		s.nextID = 1
	}
	t := Task{
		ID:          fmt.Sprintf("task-%d", s.nextID),
		Subject:     subject,
		Description: description,
		Status:      "pending",
		ActiveForm:  activeForm,
	}
	s.nextID++
	s.items = append(s.items, t)
	return t
}

// Get returns the task with id, if present.
func (s *Store) Get(id string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, t := range s.items {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}

// List returns a copy of all tasks in creation order.
func (s *Store) List() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]Task(nil), s.items...)
}

// Update changes non-empty fields on an existing task.
func (s *Store) Update(id, status, subject, description, activeForm string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, t := range s.items {
		if t.ID != id {
			continue
		}
		if status != "" {
			if !validStatus(status) {
				return Task{}, fmt.Errorf("status %q is invalid (want pending|in_progress|completed)", status)
			}
			t.Status = status
		}
		if subject != "" {
			t.Subject = subject
		}
		if description != "" {
			t.Description = description
		}
		if activeForm != "" {
			t.ActiveForm = activeForm
		}
		s.items[i] = t
		return t, nil
	}
	return Task{}, fmt.Errorf("task %q not found", id)
}

func validStatus(status string) bool {
	switch status {
	case "pending", "in_progress", "completed":
		return true
	default:
		return false
	}
}
