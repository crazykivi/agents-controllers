package agents

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"agents-controllers/backend/config"
	"agents-controllers/backend/events"
	"agents-controllers/backend/gitcmd"
	"agents-controllers/backend/store"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// StripANSI убирает escape-коды терминала из вывода дочерних процессов.
func StripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

type proc struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	done     chan struct{}
	canceled bool

	job     jobObject // windows: job object с kill-on-close; unix: всегда 0
	jobMu   sync.Mutex
	jobDone bool
}

// Supervisor запускает и останавливает процессы aider (по агенту)
// и python crew-runner (по задаче), раскладывая вывод по именованным потокам событий.
type Supervisor struct {
	cfg   config.Config
	store *store.Store
	hub   *events.Hub

	mu     sync.Mutex
	agents map[string]*proc
	tasks  map[string]*proc
}

func NewSupervisor(cfg config.Config, st *store.Store, hub *events.Hub) *Supervisor {
	return &Supervisor{
		cfg:    cfg,
		store:  st,
		hub:    hub,
		agents: map[string]*proc{},
		tasks:  map[string]*proc{},
	}
}

func (s *Supervisor) AgentRunning(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.agents[id]
	return ok
}

func (s *Supervisor) TaskRunning(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.tasks[id]
	return ok
}

func (s *Supervisor) Counts() (agents, tasks int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.agents), len(s.tasks)
}

func (s *Supervisor) publish(source, ref, agent, kind, text string) {
	s.hub.Publish(events.Event{Source: source, Ref: ref, Agent: agent, Kind: kind, Text: text})
}

// SendSystem публикует служебное событие (создан/удалён агент и т.п.).
func (s *Supervisor) SendSystem(ref, text string) {
	s.publish("system", ref, "system", "status", text)
}

// TaskNote публикует служебную заметку в поток задачи (откаты, снапшоты).
func (s *Supervisor) TaskNote(taskID, text string) {
	s.publish("crew", taskID, "crew", "status", text)
}

func envPairs(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

// argsFor собирает аргументы aider согласно правам агента.
func argsFor(a *store.Agent) []string {
	p := a.EffectivePerms()
	args := []string{"--no-check-update", "--subtree-only"}
	if p.AutoYes {
		args = append(args, "--yes-always")
	}
	if p.AutoCommits {
		args = append(args, "--auto-commits")
	} else {
		args = append(args, "--no-auto-commits")
	}
	// detect-urls — главный триггер самостоятельных скачиваний (pandoc и т.п.)
	if !p.DetectURLs {
		args = append(args, "--no-detect-urls")
	}
	return args
}

// StartAgent запускает интерактивную сессию aider в рабочей папке агента.
func (s *Supervisor) StartAgent(a *store.Agent) error {
	s.mu.Lock()
	if _, ok := s.agents[a.ID]; ok {
		s.mu.Unlock()
		return fmt.Errorf("agent %q is already running", a.Name)
	}
	s.mu.Unlock()

	if fi, err := os.Stat(a.WorkDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("workdir %q is not accessible", a.WorkDir)
	}

	args := argsFor(a)
	args = append(args, a.Flags...)

	cmd := exec.Command(s.cfg.AiderBin, args...)
	applyProcAttrs(cmd)
	cmd.Dir = a.WorkDir
	// PYTHONIOENCODING — иначе python пишет кириллицу в cp1251 вместо UTF-8.
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
	cmd.Env = append(cmd.Env, envPairs(a.Env)...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start aider: %w", err)
	}

	p := &proc{cmd: cmd, stdin: stdin, done: make(chan struct{})}
	p.job = attachJobObject(cmd)
	s.mu.Lock()
	s.agents[a.ID] = p
	s.mu.Unlock()
	s.publish("agent", a.ID, a.Name, "status", "started")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); s.pump("agent", a.ID, a.Name, stdout) }()
	go func() { defer wg.Done(); s.pump("agent", a.ID, a.Name, stderr) }()
	go func() {
		wg.Wait()
		waitErr := cmd.Wait()
		p.releaseJob()
		s.mu.Lock()
		delete(s.agents, a.ID)
		s.mu.Unlock()
		kind, text := "status", "stopped"
		if waitErr != nil {
			kind, text = "error", "exited: "+waitErr.Error()
		}
		s.publish("agent", a.ID, a.Name, kind, text)
		close(p.done)
	}()
	return nil
}

func (s *Supervisor) pump(source, ref, agent string, r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(StripANSI(sc.Text()), " \t\r")
		if line == "" {
			continue
		}
		s.publish(source, ref, agent, "log", line)
	}
}

// SendInput пишет команду/сообщение в stdin сессии aider.
func (s *Supervisor) SendInput(id, name, text string) error {
	s.mu.Lock()
	p := s.agents[id]
	s.mu.Unlock()
	if p == nil {
		return fmt.Errorf("agent is not running")
	}
	if _, err := p.stdin.Write([]byte(strings.TrimRight(text, "\n") + "\n")); err != nil {
		return fmt.Errorf("write to stdin: %w", err)
	}
	s.publish("agent", id, name, "input", text)
	return nil
}

// StopAgent закрывает stdin и убивает процесс, если он не завершился за 8 секунд.
func (s *Supervisor) StopAgent(id string) error {
	s.mu.Lock()
	p := s.agents[id]
	s.mu.Unlock()
	if p == nil {
		return fmt.Errorf("agent is not running")
	}
	_ = p.stdin.Close()
	go func() {
		select {
		case <-p.done:
		case <-time.After(8 * time.Second):
			p.killTree()
		}
	}()
	return nil
}

// --- Crew tasks ---

type CrewAgentSpec struct {
	Name       string   `json:"name"`
	Role       string   `json:"role"`
	Goal       string   `json:"goal"`
	Backstory  string   `json:"backstory"`
	WorkDir    string   `json:"workdir"`
	Model      string   `json:"model,omitempty"`
	AiderFlags []string `json:"aider_flags,omitempty"`
}

type CrewSpec struct {
	Task struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	} `json:"task"`
	AiderBin  string          `json:"aider_bin"`
	Mode      string          `json:"mode"`
	SharedDir string          `json:"shared_dir,omitempty"`
	Agents    []CrewAgentSpec `json:"agents"`
}

// StartTask запускает python crew-runner, который оркестрирует агентов (crewai)
// и шлёт JSONL-события в stdout; они раскладываются по потоку задачи.
func (s *Supervisor) StartTask(t *store.Task, ags []*store.Agent) error {
	s.mu.Lock()
	if _, ok := s.tasks[t.ID]; ok {
		s.mu.Unlock()
		return fmt.Errorf("task %s is already running", t.ID)
	}
	s.mu.Unlock()

	if fi, err := os.Stat(s.cfg.RunnerPath); err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("runner script %q not found", s.cfg.RunnerPath)
	}

	spec := CrewSpec{AiderBin: s.cfg.AiderBin, Mode: t.Mode, SharedDir: t.SharedDir}
	if spec.Mode == "" {
		spec.Mode = "sequential"
	}
	spec.Task.ID, spec.Task.Title, spec.Task.Description = t.ID, t.Title, t.Description
	for _, a := range ags {
		// Песочница задачи: если задана, все агенты работают только в ней,
		// их собственные воркдиректории игнорируются.
		workdir := a.WorkDir
		if t.WorkDir != "" {
			workdir = t.WorkDir
		}
		spec.Agents = append(spec.Agents, CrewAgentSpec{
			Name: a.Name, Role: a.Role, Goal: a.Goal, Backstory: a.Backstory,
			WorkDir: workdir, Model: a.Model, AiderFlags: a.Flags,
		})
	}

	// Git-снапшот: если песочница — репозиторий, запоминаем HEAD для
	// последующего просмотра диффов и отката.
	baseDir := ""
	baseSHA := ""
	snapDir := t.SharedDir
	if snapDir == "" && len(spec.Agents) > 0 {
		snapDir = spec.Agents[0].WorkDir
	}
	if snapDir != "" && gitcmd.IsRepo(snapDir) && gitcmd.HasCommits(snapDir) {
		if sha, err := gitcmd.HeadSHA(snapDir); err == nil {
			baseDir, baseSHA = snapDir, sha
		}
	}
	specJSON, err := json.Marshal(spec)
	if err != nil {
		return err
	}

	cmd := exec.Command(s.cfg.PythonBin, s.cfg.RunnerPath)
	applyProcAttrs(cmd)
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1", "PYTHONIOENCODING=utf-8")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start runner: %w", err)
	}
	if _, err := stdin.Write(append(specJSON, '\n')); err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("write spec: %w", err)
	}
	_ = stdin.Close()

	p := &proc{cmd: cmd, done: make(chan struct{})}
	p.job = attachJobObject(cmd)
	s.mu.Lock()
	s.tasks[t.ID] = p
	s.mu.Unlock()
	_ = s.store.UpdateTask(t.ID, func(tt *store.Task) error {
		now := time.Now().UTC()
		tt.Status = store.TaskRunning
		tt.StartedAt = &now
		tt.BaseDir, tt.BaseSHA = baseDir, baseSHA
		return nil
	})
	s.publish("crew", t.ID, "crew", "status", "started")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); s.pumpCrew(t.ID, stdout) }()
	go func() { defer wg.Done(); s.pump("crew", t.ID, "crew", stderr) }()
	go func() {
		wg.Wait()
		waitErr := cmd.Wait()
		p.releaseJob()
		s.mu.Lock()
		canceled := p.canceled
		delete(s.tasks, t.ID)
		s.mu.Unlock()
		status := store.TaskDone
		if canceled {
			status = store.TaskCanceled
		} else if waitErr != nil {
			status = store.TaskFailed
		}
		_ = s.store.UpdateTask(t.ID, func(tt *store.Task) error {
			now := time.Now().UTC()
			tt.Status = status
			tt.FinishedAt = &now
			if status == store.TaskFailed && tt.Error == "" {
				tt.Error = "runner exited: " + waitErr.Error()
			}
			return nil
		})
		kind := "status"
		if status == store.TaskFailed {
			kind = "error"
		}
		s.publish("crew", t.ID, "crew", kind, string(status))
		close(p.done)
	}()
	return nil
}

func (s *Supervisor) pumpCrew(taskID string, r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev struct {
			Kind  string `json:"kind"`
			Agent string `json:"agent"`
			Text  string `json:"text"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil || ev.Kind == "" {
			s.publish("crew", taskID, "crew", "log", StripANSI(line))
			continue
		}
		if ev.Kind == "result" {
			_ = s.store.UpdateTask(taskID, func(tt *store.Task) error {
				tt.Result = ev.Text
				return nil
			})
		}
		s.publish("crew", taskID, ev.Agent, ev.Kind, ev.Text)
	}
}

// CancelTask помечает задачу отменённой и убивает runner.
func (s *Supervisor) CancelTask(id string) error {
	s.mu.Lock()
	p := s.tasks[id]
	if p != nil {
		p.canceled = true
	}
	s.mu.Unlock()
	if p == nil {
		return fmt.Errorf("task is not running")
	}
	_ = p.cmd.Process.Kill()
	p.killTree()
	return nil
}

// StopAll останавливает все дочерние процессы (graceful shutdown).
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	agentIDs := make([]string, 0, len(s.agents))
	for id := range s.agents {
		agentIDs = append(agentIDs, id)
	}
	taskIDs := make([]string, 0, len(s.tasks))
	for id := range s.tasks {
		taskIDs = append(taskIDs, id)
	}
	s.mu.Unlock()
	for _, id := range agentIDs {
		_ = s.StopAgent(id)
	}
	for _, id := range taskIDs {
		_ = s.CancelTask(id)
	}
}
