package store

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Rule — правило авто-ответа на вопросы агентов: если текст вопроса
// содержит Pattern, отвечаем сразу (allow → y, deny → n), без человека.
type Rule struct {
	ID        string    `json:"id"`
	Pattern   string    `json:"pattern"`
	Action    string    `json:"action"` // allow | deny
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) ListRules() []*Rule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Rule, 0, len(s.rules))
	for _, r := range s.rules {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *Store) AddRule(pattern, action string) (*Rule, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || len(pattern) > 200 {
		return nil, fmt.Errorf("pattern must be 1..200 chars")
	}
	if action != "allow" && action != "deny" {
		return nil, fmt.Errorf("action must be allow or deny")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := &Rule{ID: NewID(), Pattern: pattern, Action: action, CreatedAt: time.Now().UTC()}
	s.rules[r.ID] = r
	return r, s.save("rules.json", s.rules)
}

func (s *Store) DeleteRule(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rules[id]; !ok {
		return ErrNotFound
	}
	delete(s.rules, id)
	return s.save("rules.json", s.rules)
}

// MatchRule возвращает действие первого правила, чей паттерн входит в текст.
func (s *Store) MatchRule(text string) (string, bool) {
	t := strings.ToLower(text)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.rules {
		if strings.Contains(t, strings.ToLower(r.Pattern)) {
			return r.Action, true
		}
	}
	return "", false
}
