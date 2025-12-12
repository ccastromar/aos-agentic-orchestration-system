package auth

import "context"

type Identity struct {
	UserID string
	Roles  []string
	Email  string
	Source string // "jwt" | "apikey"
}

type contextKey string

const identityContextKey contextKey = "aos.identity"

func WithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, identityContextKey, id)
}

func IdentityFromContext(ctx context.Context) (*Identity, bool) {
	id, ok := ctx.Value(identityContextKey).(*Identity)
	return id, ok
}
