package events

import (
	"sync"
	"time"
)

// Event — единый формат потока: логи/мысли/статусы, разделённые по source+ref.
type Event struct {
	ID     int64     `json:"id"`
	TS     time.Time `json:"ts"`
	Source string    `json:"source"` // agent | crew | system
	Ref    string    `json:"ref"`    // id агента или задачи
	Agent  string    `json:"agent,omitempty"`
	Kind   string    `json:"kind"` // log | thought | status | result | error | input
	Text   string    `json:"text"`
}

func Key(source, ref string) string { return source + ":" + ref }

type ring struct {
	items []Event
	max   int
}

func (r *ring) add(e Event) {
	r.items = append(r.items, e)
	if len(r.items) > r.max {
		r.items = append(r.items[:0], r.items[len(r.items)-r.max:]...)
	}
}

// Hub — широковещательная рассылка событий + кольцевые буферы истории по ключам.
type Hub struct {
	mu    sync.Mutex
	next  int64
	rings map[string]*ring
	subs  map[chan Event]struct{}
	max   int
	drops int64
}

func NewHub(max int) *Hub {
	if max < 1 {
		max = 1
	}
	return &Hub{rings: map[string]*ring{}, subs: map[chan Event]struct{}{}, max: max}
}

func (h *Hub) Publish(e Event) Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	e.ID = h.next
	if e.TS.IsZero() {
		e.TS = time.Now().UTC()
	}
	k := Key(e.Source, e.Ref)
	r := h.rings[k]
	if r == nil {
		r = &ring{max: h.max}
		h.rings[k] = r
	}
	r.add(e)
	for ch := range h.subs {
		select {
		case ch <- e:
		default:
			h.drops++
		}
	}
	return e
}

func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 256)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

func (h *Hub) History(key string, tail int) []Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rings[key]
	if r == nil {
		return nil
	}
	if tail <= 0 || tail > len(r.items) {
		tail = len(r.items)
	}
	out := make([]Event, tail)
	copy(out, r.items[len(r.items)-tail:])
	return out
}

func (h *Hub) Drops() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.drops
}
