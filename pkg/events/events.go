package events

import (
	"sync"
	"time"
)

type EventType string

const (
	JobProcessing         EventType = "queue.job.processing"
	JobProcessed          EventType = "queue.job.processed"
	JobFailed             EventType = "queue.job.failed"
	JobExceptionOccurred  EventType = "queue.job.exception"
	JobReleasedAfterError EventType = "queue.job.released"
	WorkerStopping        EventType = "queue.worker.stopping"
)

type Event struct {
	Type       EventType
	JobUUID    string
	JobName    string
	Queue      string
	Attempt    int
	Duration   time.Duration
	Error      error
	Connection string
}

type Listener func(Event)

// Bus is a concurrent-safe pub/sub event bus. All methods are safe for use from multiple goroutines.
type Bus struct {
	mu        sync.RWMutex
	listeners map[EventType][]Listener
}

func NewBus() *Bus {
	return &Bus{listeners: make(map[EventType][]Listener)}
}

func (b *Bus) On(eventType EventType, listener Listener) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.listeners == nil {
		b.listeners = make(map[EventType][]Listener)
	}
	// Copy-on-append so Fire's snapshot is never aliased by a concurrent append.
	old := b.listeners[eventType]
	fresh := make([]Listener, len(old)+1)
	copy(fresh, old)
	fresh[len(old)] = listener
	b.listeners[eventType] = fresh
}

func (b *Bus) Fire(event Event) {
	b.mu.RLock()
	listeners := b.listeners[event.Type]
	b.mu.RUnlock()
	for _, listener := range listeners {
		listener(event)
	}
}

func (b *Bus) Clear(eventType EventType) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.listeners, eventType)
}

func (b *Bus) ClearAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners = make(map[EventType][]Listener)
}
