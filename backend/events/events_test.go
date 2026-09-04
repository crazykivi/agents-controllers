package events

import (
	"testing"
	"time"
)

func TestPublishMonotonicIDs(t *testing.T) {
	h := NewHub(50)
	var prev int64
	for i := 0; i < 10; i++ {
		e := h.Publish(Event{Source: "agent", Ref: "a1", Kind: "log", Text: "x"})
		if e.ID <= prev {
			t.Fatalf("ids not monotonic: %d <= %d", e.ID, prev)
		}
		prev = e.ID
		if e.TS.IsZero() {
			t.Fatal("TS must be set")
		}
	}
}

func TestRingCapAndHistory(t *testing.T) {
	h := NewHub(5)
	for i := 0; i < 12; i++ {
		h.Publish(Event{Source: "agent", Ref: "a1", Kind: "log", Text: time.Now().String()})
	}
	got := h.History(Key("agent", "a1"), 100)
	if len(got) != 5 {
		t.Fatalf("ring must cap at 5, got %d", len(got))
	}
	tail := h.History(Key("agent", "a1"), 2)
	if len(tail) != 2 || tail[0].ID != got[3].ID || tail[1].ID != got[4].ID {
		t.Fatalf("tail mismatch: %+v vs %+v", tail, got)
	}
	if h.History(Key("agent", "missing"), 10) != nil {
		t.Fatal("missing key must return nil")
	}
}

func TestSubscribeBroadcastAndUnsub(t *testing.T) {
	h := NewHub(10)
	ch, unsub := h.Subscribe()
	defer unsub()

	h.Publish(Event{Source: "crew", Ref: "t1", Kind: "status", Text: "started"})
	select {
	case e := <-ch:
		if e.Kind != "status" || e.Ref != "t1" {
			t.Fatalf("unexpected event %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}

	unsub()
	h.Publish(Event{Source: "crew", Ref: "t1", Kind: "log", Text: "after"})
	select {
	case e := <-ch:
		t.Fatalf("closed subscriber got event %+v", e)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestDropsCounted(t *testing.T) {
	h := NewHub(10)
	ch, unsub := h.Subscribe()
	defer unsub()
	_ = ch
	for i := 0; i < 300; i++ {
		h.Publish(Event{Source: "system", Ref: "s", Kind: "log", Text: "x"})
	}
	if h.Drops() == 0 {
		t.Fatal("expected drops when subscriber buffer overflows")
	}
}
