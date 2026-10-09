package audit

import (
	"errors"
	"io"
	"sync"
	"time"
)

// MultiLogger writes the same audit event to each configured sink.
// HashForTrace keeps the local chain usable by workflows that anchor a trace.
type MultiLogger struct {
	loggers []Logger
	mu      sync.Mutex
}

func NewMultiLogger(loggers ...Logger) *MultiLogger {
	return &MultiLogger{loggers: append([]Logger(nil), loggers...)}
}

func (m *MultiLogger) Log(event Event) {
	_ = m.LogChecked(event)
}

// LogChecked serializes the write and error check so concurrent executions
// cannot accidentally consume each other's remote sink errors.
func (m *MultiLogger) LogChecked(event Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	} else {
		event.Timestamp = event.Timestamp.UTC()
	}
	var errs []error
	for _, logger := range m.loggers {
		logger.Log(event)
		if reporter, ok := logger.(ErrorReporter); ok {
			if err := reporter.Err(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (m *MultiLogger) Err() error {
	var errs []error
	for _, logger := range m.loggers {
		if reporter, ok := logger.(ErrorReporter); ok {
			if err := reporter.Err(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (m *MultiLogger) HashForTrace(traceID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, logger := range m.loggers {
		if provider, ok := logger.(HashProvider); ok {
			if hash := provider.HashForTrace(traceID); hash != "" {
				return hash
			}
		}
	}
	return ""
}

func (m *MultiLogger) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var errs []error
	for _, logger := range m.loggers {
		if closer, ok := logger.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
