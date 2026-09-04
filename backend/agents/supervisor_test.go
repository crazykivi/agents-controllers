package agents

import (
	"path/filepath"
	"testing"

	"agents-controllers/backend/config"
	"agents-controllers/backend/events"
	"agents-controllers/backend/store"
)

func newTestSupervisor(t *testing.T) *Supervisor {
	t.Helper()
	st, err := store.New(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	cfg := config.Config{
		AiderBin:   "definitely-missing-binary-42",
		PythonBin:  "definitely-missing-binary-42",
		RunnerPath: filepath.Join(t.TempDir(), "missing.py"),
		LogTail:    100,
	}
	return NewSupervisor(cfg, st, events.NewHub(100))
}

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello", "hello"},
		{"color", "\x1b[32mgreen\x1b[0m", "green"},
		{"cursor", "a\x1b[?25lb", "ab"},
		{"erase line", "x\x1b[2K", "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripANSI(tt.in); got != tt.want {
				t.Fatalf("StripANSI(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestStartAgentRejectsMissingWorkdir(t *testing.T) {
	sup := newTestSupervisor(t)
	a := &store.Agent{ID: store.NewID(), Name: "x", WorkDir: filepath.Join(t.TempDir(), "nope")}
	if err := sup.StartAgent(a); err == nil {
		t.Fatal("expected error for missing workdir")
	}
	if sup.AgentRunning(a.ID) {
		t.Fatal("agent must not be registered after failed start")
	}
}

func TestStartAgentRejectsMissingBinary(t *testing.T) {
	sup := newTestSupervisor(t)
	a := &store.Agent{ID: store.NewID(), Name: "x", WorkDir: t.TempDir()}
	if err := sup.StartAgent(a); err == nil {
		t.Fatal("expected error for missing aider binary")
	}
}

func TestSendInputAndStopRequireRunning(t *testing.T) {
	sup := newTestSupervisor(t)
	if err := sup.SendInput("nope", "nope", "hi"); err == nil {
		t.Fatal("SendInput on stopped agent must fail")
	}
	if err := sup.StopAgent("nope"); err == nil {
		t.Fatal("StopAgent on stopped agent must fail")
	}
	if err := sup.CancelTask("nope"); err == nil {
		t.Fatal("CancelTask on missing task must fail")
	}
}

func TestCounts(t *testing.T) {
	sup := newTestSupervisor(t)
	a, tk := sup.Counts()
	if a != 0 || tk != 0 {
		t.Fatalf("fresh supervisor must have zero procs, got %d/%d", a, tk)
	}
}
