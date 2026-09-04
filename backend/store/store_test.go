package store

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestAgentCRUD(t *testing.T) {
	s := newTestStore(t)
	a := &Agent{ID: NewID(), Name: "alpha", WorkDir: "/tmp", CreatedAt: time.Now().UTC()}

	if err := s.CreateAgent(a); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := s.CreateAgent(&Agent{ID: NewID(), Name: "alpha"}); err == nil {
		t.Fatal("duplicate name must fail")
	}
	got, err := s.GetAgent(a.ID)
	if err != nil || got.Name != "alpha" {
		t.Fatalf("GetAgent: %v %+v", err, got)
	}
	if _, err := s.GetAgent("nope"); err != ErrNotFound {
		t.Fatalf("missing agent: want ErrNotFound, got %v", err)
	}

	got.WorkDir = "/var"
	if err := s.UpdateAgent(got); err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}
	if list := s.ListAgents(); len(list) != 1 || list[0].WorkDir != "/var" {
		t.Fatalf("ListAgents: %+v", list)
	}
	if err := s.DeleteAgent(a.ID); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}
	if _, err := s.GetAgent(a.ID); err != ErrNotFound {
		t.Fatalf("after delete: want ErrNotFound, got %v", err)
	}
}

func TestPersistenceReload(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	s1, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a := &Agent{ID: NewID(), Name: "keep", WorkDir: "/x", CreatedAt: time.Now().UTC()}
	if err := s1.CreateAgent(a); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	s2, err := New(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err := s2.GetAgent(a.ID)
	if err != nil || got.Name != "keep" || got.WorkDir != "/x" {
		t.Fatalf("reload mismatch: %v %+v", err, got)
	}
}

func TestRunningTasksInterruptedOnLoad(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	s1, _ := New(dir)
	now := time.Now().UTC()
	tr := &Task{ID: NewID(), Title: "t", Status: TaskRunning, CreatedAt: now, StartedAt: &now}
	if err := s1.CreateTask(tr); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	s2, err := New(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err := s2.GetTask(tr.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskFailed || got.Error == "" {
		t.Fatalf("want failed+error, got %+v", got)
	}
}

func TestUpdateTask(t *testing.T) {
	s := newTestStore(t)
	tr := &Task{ID: NewID(), Title: "x", Status: TaskPending, CreatedAt: time.Now().UTC()}
	if err := s.CreateTask(tr); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	err := s.UpdateTask(tr.ID, func(tt *Task) error {
		tt.Status = TaskDone
		tt.Result = "ok"
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	got, _ := s.GetTask(tr.ID)
	if got.Status != TaskDone || got.Result != "ok" {
		t.Fatalf("got %+v", got)
	}
	if err := s.UpdateTask("missing", func(*Task) error { return nil }); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
