package update

import "sync"

type Hub struct {
	mu        sync.Mutex
	listeners map[string]map[chan struct{}]struct{}
}

func NewHub() *Hub {
	return &Hub{listeners: map[string]map[chan struct{}]struct{}{}}
}

func (h *Hub) Notify(subject string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for listener := range h.listeners[subject] {
		select {
		case listener <- struct{}{}:
		default:
		}
	}
}

func (h *Hub) Listen(subject string) (<-chan struct{}, func()) {
	listener := make(chan struct{}, 1)

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.listeners[subject] == nil {
		h.listeners[subject] = map[chan struct{}]struct{}{}
	}

	h.listeners[subject][listener] = struct{}{}

	return listener, func() {
		h.mu.Lock()
		defer h.mu.Unlock()

		delete(h.listeners[subject], listener)
		if len(h.listeners[subject]) == 0 {
			delete(h.listeners, subject)
		}
	}
}
