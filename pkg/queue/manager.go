package queue

import (
	"log/slog"

	"github.com/rjp2525/laravel-queue-go/pkg/events"
	"github.com/rjp2525/laravel-queue-go/pkg/failed"
	"github.com/rjp2525/laravel-queue-go/pkg/middleware"
)

type Manager struct {
	driver         Driver
	handlers       map[string]Handler
	defaultHandler Handler
	middleware     []middleware.Middleware
	events         *events.Bus
	failed         failed.Logger
	logger         *slog.Logger
	connection     string
}

func NewManager(driver Driver) *Manager {
	return &Manager{
		driver:     driver,
		handlers:   make(map[string]Handler),
		events:     events.NewBus(),
		failed:     failed.NullProvider{},
		logger:     slog.Default(),
		connection: DefaultConnection,
	}
}

func (m *Manager) Register(jobName string, handler Handler) { m.handlers[jobName] = handler }
func (m *Manager) RegisterDefault(handler Handler)          { m.defaultHandler = handler }
func (m *Manager) Use(mw ...middleware.Middleware)          { m.middleware = append(m.middleware, mw...) }
func (m *Manager) On(eventType events.EventType, listener events.Listener) {
	m.events.On(eventType, listener)
}
func (m *Manager) SetFailedProvider(p failed.Logger) { m.failed = p }
func (m *Manager) SetLogger(l *slog.Logger)          { m.logger = l }
func (m *Manager) SetConnection(name string)         { m.connection = name }

func (m *Manager) Worker(opts WorkerOptions) *Worker {
	return newWorker(m.driver, opts, m.handlers, m.defaultHandler, m.middleware, m.events, m.failed, m.logger, m.connection)
}

func (m *Manager) Dispatcher(opts ...DispatcherOption) *Dispatcher {
	return NewDispatcher(m.driver, opts...)
}
