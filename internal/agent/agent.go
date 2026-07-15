package agent

import (
	"context"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/bus"
)

type Agent interface {
	Start(ctx context.Context) error
	Inbox() chan bus.Message
}
