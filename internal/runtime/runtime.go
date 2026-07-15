package runtime

import (
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/llm"
)

type Runtime struct {
	SpecsLoaded bool
	LLMClient   llm.LLMClient
}
