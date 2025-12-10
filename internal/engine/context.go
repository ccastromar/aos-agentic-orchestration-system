package engine

import "time"

type PipelineContext struct {
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
	ctx.Vars[k] = v
}

func (ctx *PipelineContext) GetVar(k string) interface{} {
	return ctx.Vars[k]
}

func (ctx *PipelineContext) RecordToolOutput(toolName string, output map[string]interface{}) {
	ctx.ToolOutputs[toolName] = output
	// flatten and add to general contexto
	for k, v := range output {
		ctx.Vars[k] = v
	}
}
