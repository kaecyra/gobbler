package shopping

import (
	"sync"

	domain "github.com/kaecyra/gobbler/internal/shopping"
)

// hub fans a store change out to the event streams watching that list. A
// subscriber learns only that something changed; it reloads the list itself, so
// a burst of changes costs one reload and a slow client never holds a writer
// back.
type hub struct {
	mu   sync.Mutex
	subs map[int64]map[*subscription]struct{}
}

func newHub() *hub { return &hub{subs: map[int64]map[*subscription]struct{}{}} }

// subscription is one open event stream. C holds at most one pending wake-up.
type subscription struct {
	C chan struct{}
}

// subscribe registers a stream for list id. The returned function removes it,
// and must be called when the stream ends.
func (h *hub) subscribe(id int64) (*subscription, func()) {
	s := &subscription{C: make(chan struct{}, 1)}
	h.mu.Lock()
	if h.subs[id] == nil {
		h.subs[id] = map[*subscription]struct{}{}
	}
	h.subs[id][s] = struct{}{}
	h.mu.Unlock()
	return s, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subs[id], s)
		if len(h.subs[id]) == 0 {
			delete(h.subs, id)
		}
	}
}

// notify wakes every stream watching the changed list. It never blocks: the
// store calls it on the writer's goroutine.
func (h *hub) notify(c domain.Change) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs[c.ListID] {
		select {
		case s.C <- struct{}{}:
		default: // a wake-up is already pending; the reload will see this change too
		}
	}
}

// count returns the number of open streams, for tests that check cleanup.
func (h *hub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, m := range h.subs {
		n += len(m)
	}
	return n
}
