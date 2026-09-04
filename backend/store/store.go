package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Perms — права агента на деструктивные и внешние действия aider.
type Perms struct {
	AutoYes     bool `json:"auto_yes"`     // молча отвечать "y" на все вопросы aider (установка pandoc, add файлов и т.п.)
	AutoCommits bool `json:"auto_commits"` // aider сам делает git-коммиты
	DetectURLs  bool `json:"detect_urls"`  // переходить по ссылкам из чата (тянет pandoc/playwright)
}

// DefaultPerms — поведение для агентов без явных прав.
func DefaultPerms() Perms { return Perms{AutoYes: true, AutoCommits: true, DetectURLs: false} }

// Agent — конфигурация управляемого агента (aider-сессия + роль в crew).
type Agent struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	WorkDir   string            `json:"workdir"`
	Model     string            `json:"model,omitempty"`
	Flags     []string          `json:"flags,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Role      string            `json:"role,omitempty"`
	Goal      string            `json:"goal,omitempty"`
	Backstory string            `json:"backstory,omitempty"`
	Perms     *Perms            `json:"perms,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
}

func (a *Agent) EffectivePerms() Perms {
	if a.Perms == nil {
		return DefaultPerms()
	}
	return *a.Perms
}

type TaskStatus string

const (
	TaskPending  TaskStatus = "pending"
	TaskRunning  TaskStatus = "running"
	TaskDone     TaskStatus = "done"
	TaskFailed   TaskStatus = "failed"
	TaskCanceled TaskStatus = "canceled"
)

// Task — задача, которую выполняет crew (набор агентов через crewai runner).
type Task struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	AgentIDs    []string   `json:"agent_ids"`
	Mode        string     `json:"mode,omitempty"` // sequential | parallel
	WorkDir     string     `json:"workdir,omitempty"` // песочница задачи: если задана, ВСЕ агенты работают только в ней
	SharedDir   string     `json:"shared_dir,omitempty"`
	BaseDir     string     `json:"base_dir,omitempty"` // где сделан git-снапшот
	BaseSHA     string     `json:"base_sha,omitempty"` // HEAD на момент старта задачи
	Status      TaskStatus `json:"status"`
	Result      string     `json:"result,omitempty"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

var ErrNotFound = errors.New("not found")

type Store struct {
	mu     sync.RWMutex
	dir    string
	agents map[string]*Agent
	tasks  map[string]*Task
	rules  map[string]*Rule
}

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	s := &Store{dir: dir, agents: map[string]*Agent{}, tasks: map[string]*Task{}, rules: map[string]*Rule{}}
	if err := s.load("agents.json", &s.agents); err != nil {
		return nil, fmt.Errorf("load agents: %w", err)
	}
	if err := s.load("tasks.json", &s.tasks); err != nil {
		return nil, fmt.Errorf("load tasks: %w", err)
	}
	if err := s.load("rules.json", &s.rules); err != nil {
		return nil, fmt.Errorf("load rules: %w", err)
	}
	changed := false
	for _, t := range s.tasks {
		if t.Status == TaskRunning || t.Status == TaskPending {
			now := time.Now().UTC()
			t.Status = TaskFailed
			t.Error = "interrupted by server restart"
			t.FinishedAt = &now
			changed = true
		}
	}
	if changed {
		if err := s.save("tasks.json", s.tasks); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) load(name string, v any) error {
	b, err := os.ReadFile(filepath.Join(s.dir, name))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func (s *Store) save(name string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.dir, name+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.dir, name))
}

// NewID — короткий случайный id без внешних зависимостей.
func NewID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- Agents ---

func (s *Store) ListAgents() []*Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Agent, 0, len(s.agents))
	for _, a := range s.agents {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Store) GetAgent(id string) (*Agent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.agents[id]
	if !ok {
		return nil, ErrNotFound
	}
	return a, nil
}

func (s *Store) CreateAgent(a *Agent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.agents[a.ID]; ok {
		return fmt.Errorf("agent id already exists")
	}
	for _, x := range s.agents {
		if x.Name == a.Name {
			return fmt.Errorf("agent name %q already exists", a.Name)
		}
	}
	s.agents[a.ID] = a
	return s.save("agents.json", s.agents)
}

func (s *Store) UpdateAgent(a *Agent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.agents[a.ID]; !ok {
		return ErrNotFound
	}
	for _, x := range s.agents {
		if x.ID != a.ID && x.Name == a.Name {
			return fmt.Errorf("agent name %q already exists", a.Name)
		}
	}
	s.agents[a.ID] = a
	return s.save("agents.json", s.agents)
}

func (s *Store) DeleteAgent(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.agents[id]; !ok {
		return ErrNotFound
	}
	delete(s.agents, id)
	return s.save("agents.json", s.agents)
}

// --- Tasks ---

func (s *Store) ListTasks() []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (s *Store) GetTask(id string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, ErrNotFound
	}
	return t, nil
}

func (s *Store) CreateTask(t *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[t.ID]; ok {
		return fmt.Errorf("task id already exists")
	}
	s.tasks[t.ID] = t
	return s.save("tasks.json", s.tasks)
}

// UpdateTask мутирует задачу под замком и сохраняет её.
func (s *Store) UpdateTask(id string, fn func(*Task) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return ErrNotFound
	}
	if err := fn(t); err != nil {
		return err
	}
	return s.save("tasks.json", s.tasks)
}
