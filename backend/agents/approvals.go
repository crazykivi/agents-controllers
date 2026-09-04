package agents

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// approvalPrompt — вывод aider, означающий, что он ждёт y/n в stdin.
var approvalPrompt = regexp.MustCompile(`(?i)\(y\)es/\(n\)o|\[y/n\]|\(y/n\)|\(yes/no\)|\(y\)es, \(n\)o`)

const approvalTimeout = 3 * time.Minute

// Approval — вопрос процесса, ждущий решения человека.
type Approval struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	AgentName string    `json:"agent_name"`
	Text      string    `json:"text"`
	TS        time.Time `json:"ts"`
	answer    chan string
}

// Approvals — реестр ожидающих вопросов. Ответ приходит каналом от HTTP-ручки.
type Approvals struct {
	mu sync.Mutex
	m  map[string]*Approval
}

func NewApprovals() *Approvals {
	return &Approvals{m: map[string]*Approval{}}
}

func (a *Approvals) add(ap *Approval) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// один активный вопрос на агента — старый (зависший) вытесняется
	for id, old := range a.m {
		if old.AgentID == ap.AgentID {
			close(old.answer)
			delete(a.m, id)
		}
	}
	a.m[ap.ID] = ap
}

func (a *Approvals) resolve(id, decision string) (*Approval, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ap, ok := a.m[id]
	if !ok {
		return nil, false
	}
	delete(a.m, id)
	ap.answer <- decision
	close(ap.answer)
	return ap, true
}

func (a *Approvals) list() []*Approval {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]*Approval, 0, len(a.m))
	for _, ap := range a.m {
		out = append(out, ap)
	}
	return out
}

// questionMatch проверяет строку вывода и нормализует текст вопроса.
func questionMatch(line string) (string, bool) {
	if !approvalPrompt.MatchString(line) {
		return "", false
	}
	t := strings.TrimSpace(line)
	if len(t) > 300 {
		t = t[:300]
	}
	return t, true
}

var errApprovalClosed = fmt.Errorf("approval cancelled")
