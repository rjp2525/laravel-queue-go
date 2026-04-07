package events

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBusOnAndFire(t *testing.T) {
	bus := NewBus()

	var received Event
	bus.On(JobProcessed, func(e Event) {
		received = e
	})

	bus.Fire(Event{
		Type:    JobProcessed,
		JobUUID: "uuid-1",
		JobName: "TestJob",
		Queue:   "default",
	})

	assert.Equal(t, JobProcessed, received.Type)
	assert.Equal(t, "uuid-1", received.JobUUID)
	assert.Equal(t, "TestJob", received.JobName)
	assert.Equal(t, "default", received.Queue)
}

func TestBusMultipleListeners(t *testing.T) {
	bus := NewBus()

	var count int
	bus.On(JobFailed, func(e Event) { count++ })
	bus.On(JobFailed, func(e Event) { count++ })
	bus.On(JobFailed, func(e Event) { count++ })

	bus.Fire(Event{Type: JobFailed})
	assert.Equal(t, 3, count)
}

func TestBusNoListenersDoesNotPanic(t *testing.T) {
	bus := NewBus()
	assert.NotPanics(t, func() {
		bus.Fire(Event{Type: JobProcessing})
	})
}

func TestBusEventTypeIsolation(t *testing.T) {
	bus := NewBus()

	var called bool
	bus.On(JobProcessed, func(e Event) { called = true })

	// Fire a different event type.
	bus.Fire(Event{Type: JobFailed})
	assert.False(t, called)
}

func TestBusClear(t *testing.T) {
	bus := NewBus()

	var called bool
	bus.On(JobProcessed, func(e Event) { called = true })
	bus.Clear(JobProcessed)

	bus.Fire(Event{Type: JobProcessed})
	assert.False(t, called)
}

func TestBusClearAll(t *testing.T) {
	bus := NewBus()

	var count int
	bus.On(JobProcessed, func(e Event) { count++ })
	bus.On(JobFailed, func(e Event) { count++ })
	bus.On(WorkerStopping, func(e Event) { count++ })

	bus.ClearAll()

	bus.Fire(Event{Type: JobProcessed})
	bus.Fire(Event{Type: JobFailed})
	bus.Fire(Event{Type: WorkerStopping})
	assert.Equal(t, 0, count)
}

func TestBusClearOnlyTargetType(t *testing.T) {
	bus := NewBus()

	var processedCalled, failedCalled bool
	bus.On(JobProcessed, func(e Event) { processedCalled = true })
	bus.On(JobFailed, func(e Event) { failedCalled = true })

	bus.Clear(JobProcessed)

	bus.Fire(Event{Type: JobProcessed})
	bus.Fire(Event{Type: JobFailed})

	assert.False(t, processedCalled)
	assert.True(t, failedCalled)
}
