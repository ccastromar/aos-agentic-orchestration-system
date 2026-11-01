package runtime

import (
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/llm"
)

type Runtime struct {
	SpecsLoaded bool
	LLMClient   llm.LLMClient

	// What this deployment was started as, reported by /health/ready.
	Execution  string
	Governance string
	Evidence   string
}
