package auth

import (
	"net/http"
)

type Authenticator interface {
	Authenticate(r *http.Request) (*Identity, error)
}

type AuthError struct {
	Code    int
	Message string
}

func (e *AuthError) Error() string {
	return e.Message
}

type Chain struct {
	Authenticators []Authenticator
}

func (c *Chain) Authenticate(r *http.Request) (*Identity, error) {
	var lastErr error
	for _, a := range c.Authenticators {
		id, err := a.Authenticate(r)
		if err == nil {
			return id, nil
		}
		lastErr = err
	}

	if lastErr != nil {
		if ae, ok := lastErr.(*AuthError); ok {
			return nil, ae
		}
	}

	return nil, &AuthError{Code: http.StatusUnauthorized, Message: "authentication failed"}
}

// HTTP middleware
func Middleware(chain *Chain, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := chain.Authenticate(r)
		if err != nil {
			if ae, ok := err.(*AuthError); ok {
				http.Error(w, ae.Message, ae.Code)
				return
			}
			http.Error(w, "authentication error", http.StatusUnauthorized)
			return
		}
		ctx := WithIdentity(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
