package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"agents-controllers/backend/agents"
	"agents-controllers/backend/config"
	"agents-controllers/backend/events"
	"agents-controllers/backend/gitcmd"
	"agents-controllers/backend/middleware"
	"agents-controllers/backend/store"
	"github.com/gin-gonic/gin"
)

var (
	nameRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	flagRe   = regexp.MustCompile(`^-{1,2}[A-Za-z0-9][A-Za-z0-9_.:=/@,-]{0,126}$`)
	envKeyRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
)

type Server struct {
	cfg   config.Config
	store *store.Store
	sup   *agents.Supervisor
	hub   *events.Hub
	start time.Time
}

func NewRouter(cfg config.Config, st *store.Store, sup *agents.Supervisor, hub *events.Hub) *gin.Engine {
	s := &Server{cfg: cfg, store: st, sup: sup, hub: hub, start: time.Now()}
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	if len(cfg.TrustedProxies) > 0 {
		_ = r.SetTrustedProxies(cfg.TrustedProxies)
	} else {
		_ = r.SetTrustedProxies(nil)
	}

	limiter := middleware.New(10, 30, 1, 5)
	stopCleanup := make(chan struct{})
	limiter.StartCleanup(5*time.Minute, 15*time.Minute, stopCleanup)

	api := r.Group("/api", s.cors(), limiter.Middleware())
	{
		api.GET("/health", s.health)
		api.GET("/events", s.stream)

		api.GET("/agents", s.listAgents)
		api.POST("/agents", s.createAgent)
		api.GET("/agents/:id", s.getAgent)
		api.PUT("/agents/:id", s.updateAgent)
		api.DELETE("/agents/:id", s.deleteAgent)
		api.POST("/agents/:id/start", limiter.Strict(), s.startAgent)
		api.POST("/agents/:id/stop", s.stopAgent)
		api.POST("/agents/:id/input", limiter.Strict(), s.agentInput)
		api.GET("/agents/:id/logs", s.agentLogs)
		api.GET("/agents/:id/git/status", s.agentGitStatus)
		api.GET("/agents/:id/git/diff", s.agentGitDiff)
		api.POST("/agents/:id/git/undo", limiter.Strict(), s.agentGitUndo)

		api.GET("/approvals", s.listApprovals)
		api.POST("/approvals/:id", limiter.Strict(), s.resolveApproval)

		api.GET("/rules", s.listRules)
		api.POST("/rules", limiter.Strict(), s.addRule)
		api.DELETE("/rules/:id", s.deleteRule)

		api.GET("/tasks", s.listTasks)
		api.POST("/tasks", limiter.Strict(), s.createTask)
		api.GET("/tasks/:id", s.getTask)
		api.DELETE("/tasks/:id", s.deleteTask)
		api.POST("/tasks/:id/cancel", limiter.Strict(), s.cancelTask)
		api.POST("/tasks/:id/approve", limiter.Strict(), s.approveTask)
		api.POST("/tasks/:id/restart", limiter.Strict(), s.restartTask)
		api.GET("/tasks/:id/logs", s.taskLogs)
		api.GET("/tasks/:id/git/status", s.taskGitStatus)
		api.GET("/tasks/:id/git/diff", s.taskGitDiff)
		api.POST("/tasks/:id/git/rollback", limiter.Strict(), s.taskGitRollback)

		api.GET("/templates", s.listTemplates)
		api.POST("/templates", limiter.Strict(), s.createTemplate)
		api.DELETE("/templates/:id", s.deleteTemplate)
	}
	s.mountStatic(r)
	return r
}

func (s *Server) cors() gin.HandlerFunc {
	allowed := make(map[string]bool, len(s.cfg.AllowCORS))
	for _, o := range s.cfg.AllowCORS {
		allowed[o] = true
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			if !allowed[origin] {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "origin not allowed"})
				return
			}
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Headers", "Content-Type")
			c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
			if c.Request.Method == http.MethodOptions {
				c.AbortWithStatus(http.StatusNoContent)
				return
			}
		}
		c.Next()
	}
}

func (s *Server) health(c *gin.Context) {
	a, t := s.sup.Counts()
	c.JSON(http.StatusOK, gin.H{
		"status":      "ok",
		"uptime_s":    int(time.Since(s.start).Seconds()),
		"agents":      a,
		"tasks":       t,
		"event_drops": s.hub.Drops(),
	})
}

// --- validation ---

type agentReq struct {
	Name      string            `json:"name"`
	WorkDir   string            `json:"workdir"`
	Model     string            `json:"model"`
	Flags     []string          `json:"flags"`
	Env       map[string]string `json:"env"`
	Role      string            `json:"role"`
	Goal      string            `json:"goal"`
	Backstory string            `json:"backstory"`
	Perms     *store.Perms      `json:"perms"`
}

var winAbsRe = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

func isExistingDir(p string) bool {
	if !strings.HasPrefix(p, "/") && !winAbsRe.MatchString(p) {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func validateAgent(r *agentReq) error {
	if !nameRe.MatchString(r.Name) {
		return errors.New("invalid name: [A-Za-z0-9][A-Za-z0-9_.-]{0,63}")
	}
	if !isExistingDir(r.WorkDir) {
		return errors.New("workdir must be an existing absolute path")
	}
	if len(r.Model) > 100 || strings.ContainsAny(r.Model, "\n\r") {
		return errors.New("invalid model")
	}
	if len(r.Flags) > 20 {
		return errors.New("too many flags (max 20)")
	}
	for _, f := range r.Flags {
		if !flagRe.MatchString(f) {
			return errors.New("invalid flag: " + f)
		}
	}
	for k, v := range r.Env {
		if !envKeyRe.MatchString(k) || strings.ContainsAny(v, "\n\r\x00") {
			return errors.New("invalid env entry: " + k)
		}
	}
	if len(r.Role) > 200 || len(r.Goal) > 500 || len(r.Backstory) > 2000 {
		return errors.New("role/goal/backstory too long")
	}
	if strings.ContainsAny(r.Role+r.Goal+r.Backstory, "\x00") {
		return errors.New("invalid control chars")
	}
	return nil
}

func agentView(a *store.Agent, running bool) gin.H {
	return gin.H{
		"id": a.ID, "name": a.Name, "workdir": a.WorkDir, "model": a.Model,
		"flags": a.Flags, "env": a.Env, "role": a.Role, "goal": a.Goal,
		"backstory": a.Backstory, "created_at": a.CreatedAt,
		"perms":  a.EffectivePerms(),
		"status": map[bool]string{true: "running", false: "stopped"}[running],
	}
}

// --- agents handlers ---

func (s *Server) listAgents(c *gin.Context) {
	out := make([]gin.H, 0)
	for _, a := range s.store.ListAgents() {
		out = append(out, agentView(a, s.sup.AgentRunning(a.ID)))
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) getAgent(c *gin.Context) {
	a, err := s.store.GetAgent(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, agentView(a, s.sup.AgentRunning(a.ID)))
}

func (s *Server) createAgent(c *gin.Context) {
	var req agentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if err := validateAgent(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	a := &store.Agent{
		ID: store.NewID(), Name: req.Name, WorkDir: req.WorkDir, Model: req.Model,
		Flags: req.Flags, Env: req.Env, Role: req.Role, Goal: req.Goal,
		Backstory: req.Backstory, Perms: req.Perms, CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateAgent(a); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	s.sup.SendSystem("agent", "created: "+a.Name)
	c.JSON(http.StatusCreated, agentView(a, false))
}

func (s *Server) updateAgent(c *gin.Context) {
	a, err := s.store.GetAgent(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if s.sup.AgentRunning(a.ID) {
		c.JSON(http.StatusConflict, gin.H{"error": "stop the agent before editing"})
		return
	}
	var req agentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if err := validateAgent(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	a.Name, a.WorkDir, a.Model = req.Name, req.WorkDir, req.Model
	a.Flags, a.Env = req.Flags, req.Env
	a.Role, a.Goal, a.Backstory = req.Role, req.Goal, req.Backstory
	a.Perms = req.Perms
	if err := s.store.UpdateAgent(a); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, agentView(a, false))
}

func (s *Server) deleteAgent(c *gin.Context) {
	a, err := s.store.GetAgent(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if s.sup.AgentRunning(a.ID) {
		c.JSON(http.StatusConflict, gin.H{"error": "stop the agent first"})
		return
	}
	if err := s.store.DeleteAgent(a.ID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) startAgent(c *gin.Context) {
	a, err := s.store.GetAgent(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err := s.sup.StartAgent(a); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, agentView(a, true))
}

func (s *Server) stopAgent(c *gin.Context) {
	if err := s.sup.StopAgent(c.Param("id")); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) agentInput(c *gin.Context) {
	a, err := s.store.GetAgent(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" || len(req.Text) > 8192 || strings.ContainsRune(req.Text, '\x00') {
		c.JSON(http.StatusBadRequest, gin.H{"error": "text must be 1..8192 chars"})
		return
	}
	if err := s.sup.SendInput(a.ID, a.Name, req.Text); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) tailParam(c *gin.Context) int {
	tail := 500
	if v := c.Query("tail"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			tail = n
		}
	}
	if tail > s.cfg.LogTail {
		tail = s.cfg.LogTail
	}
	return tail
}

func (s *Server) agentLogs(c *gin.Context) {
	if _, err := s.store.GetAgent(c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, s.hub.History(events.Key("agent", c.Param("id")), s.tailParam(c)))
}

// --- git handlers ---

const maxDiffBytes = 256 * 1024

func truncateDiff(d string) string {
	if len(d) > maxDiffBytes {
		return d[:maxDiffBytes] + "\n… (обрезано)"
	}
	return d
}

func gitView(dir string) (bool, string, []gitcmd.FileChange) {
	if dir == "" || !gitcmd.IsRepo(dir) {
		return false, "", []gitcmd.FileChange{}
	}
	branch, _ := gitcmd.Branch(dir)
	files, err := gitcmd.Status(dir)
	if err != nil {
		files = []gitcmd.FileChange{}
	}
	return true, branch, files
}

func (s *Server) agentGitStatus(c *gin.Context) {
	a, err := s.store.GetAgent(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	repo, branch, files := gitView(a.WorkDir)
	c.JSON(http.StatusOK, gin.H{"repo": repo, "branch": branch, "changes": files})
}

func (s *Server) agentGitDiff(c *gin.Context) {
	a, err := s.store.GetAgent(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if !gitcmd.IsRepo(a.WorkDir) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "workdir is not a git repository"})
		return
	}
	d, err := gitcmd.Diff(a.WorkDir, "")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"diff": truncateDiff(d)})
}

func (s *Server) agentGitUndo(c *gin.Context) {
	a, err := s.store.GetAgent(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err := s.sup.SendInput(a.ID, a.Name, "/undo"); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) taskGitStatus(c *gin.Context) {
	t, err := s.store.GetTask(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	repo, branch, files := gitView(t.BaseDir)
	c.JSON(http.StatusOK, gin.H{"repo": repo, "branch": branch, "changes": files})
}

func (s *Server) taskGitDiff(c *gin.Context) {
	t, err := s.store.GetTask(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if t.BaseDir == "" || !gitcmd.IsRepo(t.BaseDir) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "git snapshot is not available for this task"})
		return
	}
	d, err := gitcmd.Diff(t.BaseDir, t.BaseSHA)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"diff": truncateDiff(d)})
}

func (s *Server) taskGitRollback(c *gin.Context) {
	t, err := s.store.GetTask(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if t.Status == store.TaskRunning {
		c.JSON(http.StatusConflict, gin.H{"error": "остановите задачу перед откатом"})
		return
	}
	if t.BaseDir == "" || t.BaseSHA == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "git snapshot is not available for this task"})
		return
	}
	if err := gitcmd.Reset(t.BaseDir, t.BaseSHA); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	s.sup.TaskNote(t.ID, "git: рабочий каталог откачен к снапшоту "+t.BaseSHA)
	t, _ = s.store.GetTask(t.ID)
	c.JSON(http.StatusOK, t)
}

// --- approvals / rules ---

func (s *Server) listApprovals(c *gin.Context) {
	aps := s.sup.PendingApprovals()
	out := make([]gin.H, 0, len(aps))
	for _, ap := range aps {
		out = append(out, gin.H{
			"id": ap.ID, "agent_id": ap.AgentID, "agent_name": ap.AgentName,
			"text": ap.Text, "ts": ap.TS,
		})
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) resolveApproval(c *gin.Context) {
	var req struct {
		Action string `json:"action"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || (req.Action != "allow" && req.Action != "deny") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "action must be allow or deny"})
		return
	}
	if err := s.sup.ResolveApproval(c.Param("id"), req.Action); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *Server) listRules(c *gin.Context) {
	c.JSON(http.StatusOK, s.store.ListRules())
}

func (s *Server) addRule(c *gin.Context) {
	var req struct {
		Pattern string `json:"pattern"`
		Action  string `json:"action"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	r, err := s.store.AddRule(req.Pattern, req.Action)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, r)
}

func (s *Server) deleteRule(c *gin.Context) {
	if err := s.store.DeleteRule(c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// --- tasks handlers ---

type taskReq struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	AgentIDs    []string `json:"agent_ids"`
	Mode        string   `json:"mode"`
	WorkDir     string   `json:"workdir"`
	SharedDir   string   `json:"shared_dir"`
	ConfirmPlan bool     `json:"confirm_plan"`
	DependsOn   []string `json:"depends_on"`
}

func (s *Server) listTasks(c *gin.Context) {
	out := make([]*store.Task, 0)
	for _, t := range s.store.ListTasks() {
		out = append(out, t)
	}
	c.JSON(http.StatusOK, out)
}

func (s *Server) getTask(c *gin.Context) {
	t, err := s.store.GetTask(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, t)
}

func (s *Server) createTask(c *gin.Context) {
	var req taskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" || len(req.Title) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "title must be 1..200 chars"})
		return
	}
	if len(req.Description) == 0 || len(req.Description) > 32000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "description must be 1..32000 chars"})
		return
	}
	if len(req.AgentIDs) == 0 || len(req.AgentIDs) > 10 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "select 1..10 agents"})
		return
	}
	ags := make([]*store.Agent, 0, len(req.AgentIDs))
	for _, id := range req.AgentIDs {
		a, err := s.store.GetAgent(id)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown agent: " + id})
			return
		}
		ags = append(ags, a)
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = "sequential"
	}
	if mode != "sequential" && mode != "parallel" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "mode must be sequential or parallel"})
		return
	}
	shared := strings.TrimSpace(req.SharedDir)
	if shared != "" && !isExistingDir(shared) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "shared_dir must be an existing absolute path"})
		return
	}
	// Песочница задачи: если указана, ВСЕ агенты работают только в ней.
	workdir := strings.TrimSpace(req.WorkDir)
	if workdir != "" && !isExistingDir(workdir) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "workdir must be an existing absolute path"})
		return
	}
	if mode == "parallel" && shared == "" {
		shared = workdir
		if shared == "" {
			shared = ags[0].WorkDir
		}
	}
	// dry-run без плана не бывает: план пишет только параллельный координатор
	confirmPlan := req.ConfirmPlan && mode == "parallel"
	// зависимости: существующие задачи, отличные от себя
	queued := false
	seenDeps := map[string]bool{}
	for _, dep := range req.DependsOn {
		if dep == "" || seenDeps[dep] {
			continue
		}
		seenDeps[dep] = true
		d, err := s.store.GetTask(dep)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "неизвестная задача-зависимость: " + dep})
			return
		}
		switch d.Status {
		case store.TaskPending, store.TaskRunning, store.TaskAwaiting:
			queued = true
		case store.TaskFailed, store.TaskCanceled:
			c.JSON(http.StatusBadRequest, gin.H{"error": "зависимость " + dep + " провалена — запуск бессмыслен"})
			return
		}
	}
	newID := store.NewID()
	for dep := range seenDeps {
		if dep == newID {
			c.JSON(http.StatusBadRequest, gin.H{"error": "задача не может зависеть от самой себя"})
			return
		}
	}
	t := &store.Task{
		ID: newID, Title: req.Title, Description: req.Description,
		AgentIDs: req.AgentIDs, Mode: mode, WorkDir: workdir, SharedDir: shared,
		ConfirmPlan: confirmPlan, DependsOn: req.DependsOn,
		Status: store.TaskPending, CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateTask(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if queued {
		// запустится kickQueue'ем, когда зависимости разрешатся
		t, _ = s.store.GetTask(t.ID)
		c.JSON(http.StatusCreated, t)
		return
	}
	if err := s.sup.StartTask(t, ags); err != nil {
		_ = s.store.UpdateTask(t.ID, func(tt *store.Task) error {
			tt.Status = store.TaskFailed
			tt.Error = err.Error()
			return nil
		})
		t, _ = s.store.GetTask(t.ID)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "task": t})
		return
	}
	t, _ = s.store.GetTask(t.ID)
	c.JSON(http.StatusCreated, t)
}

func (s *Server) cancelTask(c *gin.Context) {
	t, err := s.store.GetTask(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if err := s.sup.CancelTask(t.ID); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	t, _ = s.store.GetTask(t.ID)
	c.JSON(http.StatusOK, t)
}

func (s *Server) taskLogs(c *gin.Context) {
	if _, err := s.store.GetTask(c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, s.hub.History(events.Key("crew", c.Param("id")), s.tailParam(c)))
}

func (s *Server) deleteTask(c *gin.Context) {
	t, err := s.store.GetTask(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if t.Status == store.TaskRunning || t.Status == store.TaskAwaiting {
		c.JSON(http.StatusConflict, gin.H{"error": "остановите задачу перед удалением"})
		return
	}
	// задача не должна быть чьей-то зависимостью
	for _, x := range s.store.ListTasks() {
		if x.ID == t.ID {
			continue
		}
		for _, dep := range x.DependsOn {
			if dep == t.ID && (x.Status == store.TaskPending) {
				c.JSON(http.StatusConflict, gin.H{"error": "задача является зависимостью " + x.ID + " — сначала удалите её"})
				return
			}
		}
	}
	if err := s.store.DeleteTask(t.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// restartTask запускает копию завершённой задачи (снимок её конфигурации).
func (s *Server) restartTask(c *gin.Context) {
	t, err := s.store.GetTask(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	ags := make([]*store.Agent, 0, len(t.AgentIDs))
	for _, id := range t.AgentIDs {
		a, err := s.store.GetAgent(id)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "агент недоступен: " + id})
			return
		}
		ags = append(ags, a)
	}
	nt := &store.Task{
		ID: store.NewID(), Title: t.Title, Description: t.Description,
		AgentIDs: t.AgentIDs, Mode: t.Mode, WorkDir: t.WorkDir, SharedDir: t.SharedDir,
		ConfirmPlan: t.ConfirmPlan, DependsOn: nil,
		Status: store.TaskPending, CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateTask(nt); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := s.sup.StartTask(nt, ags); err != nil {
		_ = s.store.UpdateTask(nt.ID, func(tt *store.Task) error {
			tt.Status = store.TaskFailed
			tt.Error = err.Error()
			return nil
		})
		nt, _ = s.store.GetTask(nt.ID)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error(), "task": nt})
		return
	}
	nt, _ = s.store.GetTask(nt.ID)
	c.JSON(http.StatusCreated, nt)
}

func (s *Server) approveTask(c *gin.Context) {
	t, err := s.store.GetTask(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	if t.Status != store.TaskAwaiting {
		c.JSON(http.StatusConflict, gin.H{"error": "задача не ждёт подтверждения плана"})
		return
	}
	var req struct {
		Approve bool `json:"approve"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if err := s.sup.ApprovePlan(t.ID, req.Approve); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	_ = s.store.UpdateTask(t.ID, func(tt *store.Task) error {
		tt.Status = store.TaskRunning
		return nil
	})
	t, _ = s.store.GetTask(t.ID)
	c.JSON(http.StatusOK, t)
}

// --- templates handlers ---

func (s *Server) listTemplates(c *gin.Context) {
	c.JSON(http.StatusOK, s.store.ListTemplates())
}

func (s *Server) createTemplate(c *gin.Context) {
	var req struct {
		Name    string  `json:"name"`
		TaskID  *string `json:"task_id"`
		Payload *struct {
			Title       string   `json:"title"`
			Description string   `json:"description"`
			AgentIDs    []string `json:"agent_ids"`
			Mode        string   `json:"mode"`
			WorkDir     string   `json:"workdir"`
			SharedDir   string   `json:"shared_dir"`
			ConfirmPlan bool     `json:"confirm_plan"`
		} `json:"payload"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	tpl := &store.Template{ID: store.NewID(), Name: req.Name, CreatedAt: time.Now().UTC()}
	if req.TaskID != nil && *req.TaskID != "" {
		t, err := s.store.GetTask(*req.TaskID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
			return
		}
		tpl.Title, tpl.Description, tpl.AgentIDs = t.Title, t.Description, t.AgentIDs
		tpl.Mode, tpl.WorkDir, tpl.SharedDir = t.Mode, t.WorkDir, t.SharedDir
		tpl.ConfirmPlan = t.ConfirmPlan
	} else if req.Payload != nil {
		p := req.Payload
		tpl.Title, tpl.Description, tpl.AgentIDs = p.Title, p.Description, p.AgentIDs
		tpl.Mode, tpl.WorkDir, tpl.SharedDir = p.Mode, p.WorkDir, p.SharedDir
		tpl.ConfirmPlan = p.ConfirmPlan
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "нужен task_id или payload"})
		return
	}
	if err := s.store.CreateTemplate(tpl); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, tpl)
}

func (s *Server) deleteTemplate(c *gin.Context) {
	if err := s.store.DeleteTemplate(c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// --- SSE ---

func (s *Server) stream(c *gin.Context) {
	fl, ok := c.Writer.(http.Flusher)
	if !ok {
		c.String(http.StatusInternalServerError, "streaming unsupported")
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	ch, unsub := s.hub.Subscribe()
	defer unsub()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	notify := c.Request.Context().Done()
	for {
		select {
		case <-notify:
			return
		case <-ticker.C:
			_, _ = c.Writer.Write([]byte(": ping\n\n"))
			fl.Flush()
		case ev := <-ch:
			b, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			_, _ = c.Writer.Write(append(append([]byte("data: "), b...), '\n', '\n'))
			fl.Flush()
		}
	}
}

// --- static SPA ---

func (s *Server) mountStatic(r *gin.Engine) {
	dir := s.cfg.StaticDir
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		r.NoRoute(func(c *gin.Context) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		})
		return
	}
	index := filepath.Join(dir, "index.html")
	r.NoRoute(func(c *gin.Context) {
		p := path.Clean("/" + c.Request.URL.Path)
		if strings.HasPrefix(p, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if p == "/" {
			c.File(index)
			return
		}
		full := filepath.Join(dir, filepath.FromSlash(p))
		if !strings.HasPrefix(full, filepath.Clean(dir)+string(os.PathSeparator)) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad path"})
			return
		}
		if fi, err := os.Stat(full); err == nil && !fi.IsDir() {
			c.File(full)
			return
		}
		c.File(index)
	})
}
