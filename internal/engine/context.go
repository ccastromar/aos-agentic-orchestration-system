package engine

import (
	"fmt"
	"sync"
	"time"
)

type PipelineContext struct {
	mu          sync.RWMutex
	Vars        map[string]interface{}
	ToolOutputs map[string]map[string]interface{} // to debug or audit
	Meta        map[string]interface{}            // timing, flags, etc.
	Now         time.Time
}

func NewPipelineContext(initial map[string]interface{}) *PipelineContext {
	ctx := &PipelineContext{
		Vars:        make(map[string]interface{}),
		ToolOutputs: make(map[string]map[string]interface{}),
		Meta:        make(map[string]interface{}),
		Now:         time.Now(),
	}

	for k, v := range initial {
		ctx.Vars[k] = v
	}

	ctx.Meta["started_at"] = ctx.Now
	return ctx
}

func (ctx *PipelineContext) SetVar(k string, v interface{}) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.Vars[k] = v
}

func (ctx *PipelineContext) GetVar(k string) interface{} {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.Vars[k]
}

func (ctx *PipelineContext) RecordToolOutput(toolName string, output map[string]interface{}) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.ToolOutputs[toolName] = output
	// flatten and add to general context
	for k, v := range output {
		switch k {
		case "ok", "statusCode", "status", "raw":
			// namespacing
			key := fmt.Sprintf("%s.%s", toolName, k)
			ctx.Vars[key] = v

		default:
			// business variables without namespacing
			ctx.Vars[k] = v
		}
	}
}

// GetVarsSnapshot returns a copy of the current variables
func (ctx *PipelineContext) GetVarsSnapshot() map[string]interface{} {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	snapshot := make(map[string]interface{}, len(ctx.Vars))
	for k, v := range ctx.Vars {
		snapshot[k] = v
	}
	return snapshot
}

// ReplaceVars replaces all variables
func (ctx *PipelineContext) ReplaceVars(newVars map[string]interface{}) {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.Vars = newVars
}
