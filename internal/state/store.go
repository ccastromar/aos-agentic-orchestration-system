package state

import "context"

type Store interface {
	Save(ctx context.Context, st *ExecutionState) error
	Load(ctx context.Context, id string) (*ExecutionState, error)
	Delete(ctx context.Context, id string) error
}
