package store

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Template — сохранённая конфигурация задачи для переиспользования.
type Template struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	AgentIDs    []string  `json:"agent_ids"`
	Mode        string    `json:"mode,omitempty"`
	WorkDir     string    `json:"workdir,omitempty"`
	SharedDir   string    `json:"shared_dir,omitempty"`
	ConfirmPlan bool      `json:"confirm_plan,omitempty"`
	DependsOn   []string  `json:"depends_on,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Store) ListTemplates() []*Template {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Template, 0, len(s.templates))
	for _, t := range s.templates {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *Store) GetTemplate(id string) (*Template, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.templates[id]
	if !ok {
		return nil, ErrNotFound
	}
	return t, nil
}

func (s *Store) CreateTemplate(t *Template) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.templates[t.ID]; ok {
		return fmt.Errorf("template id already exists")
	}
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		return fmt.Errorf("template name is required")
	}
	s.templates[t.ID] = t
	return s.save("templates.json", s.templates)
}

func (s *Store) DeleteTemplate(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.templates[id]; !ok {
		return ErrNotFound
	}
	delete(s.templates, id)
	return s.save("templates.json", s.templates)
}
