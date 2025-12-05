package agent

import (
	"context"
	"sync"
	"time"
)

// Simple per-task context registry to enable cancellation and deadline propagation.
var (
	taskCtxMu  sync.RWMutex
	taskCtx    = make(map[string]context.Context)
	taskCancel = make(map[string]context.CancelFunc)
)

// NewTaskContext creates and stores a cancelable context for a task id with the given timeout.
func NewTaskContext(parent context.Context, id string, timeout time.Duration) context.Context {
	if parent == nil {
		parent = context.Background()
	}

	var ctx context.Context
	var cancel context.CancelFunc

	if timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, timeout)
	} else {
		// TTL == 0 → NEVER EXPIRES unless explicitly canceled
		ctx, cancel = context.WithCancel(parent)
	}

	taskCtxMu.Lock()
	taskCtx[id] = ctx
	taskCancel[id] = cancel
	taskCtxMu.Unlock()

	return ctx
}

// GetTaskContext retrieves the context for a task id, if any.
func GetTaskContext(id string) (context.Context, bool) {
	taskCtxMu.RLock()
	ctx, ok := taskCtx[id]
	taskCtxMu.RUnlock()
	return ctx, ok
}

// CancelTask cancels and removes a task context.
func CancelTask(id string) {
	taskCtxMu.Lock()
	if c, ok := taskCancel[id]; ok {
		c()
	}
	delete(taskCancel, id)
	delete(taskCtx, id)
	taskCtxMu.Unlock()
}
