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

		api.GET("/tasks", s.listTasks)
		api.POST("/tasks", limiter.Strict(), s.createTask)
		api.GET("/tasks/:id", s.getTask)
		api.POST("/tasks/:id/cancel", limiter.Strict(), s.cancelTask)
		api.GET("/tasks/:id/logs", s.taskLogs)
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
		Backstory: req.Backstory, CreatedAt: time.Now().UTC(),
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

// --- tasks handlers ---

type taskReq struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	AgentIDs    []string `json:"agent_ids"`
	Mode        string   `json:"mode"`
	WorkDir     string   `json:"workdir"`
	SharedDir   string   `json:"shared_dir"`
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
	t := &store.Task{
		ID: store.NewID(), Title: req.Title, Description: req.Description,
		AgentIDs: req.AgentIDs, Mode: mode, WorkDir: workdir, SharedDir: shared,
		Status: store.TaskPending, CreatedAt: time.Now().UTC(),
	}
	if err := s.store.CreateTask(t); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
