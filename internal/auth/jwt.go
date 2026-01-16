package auth

import (
	"net/http"
	"strings"
	"time"
)

type JWTConfig struct {
	Issuer   string
	Audience string
	JWKURL   string // OIDC
}

type JWTAuthenticator struct {
	cfg JWTConfig
	// jwkCache ...
}

func NewJWTAuthenticator(cfg JWTConfig) *JWTAuthenticator {
	return &JWTAuthenticator{cfg: cfg}
}

func (a *JWTAuthenticator) Authenticate(r *http.Request) (*Identity, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, &AuthError{Code: http.StatusUnauthorized, Message: "missing bearer token"}
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	tokenString = strings.TrimSpace(tokenString)

	// TODO: implement verifyAndParseJWT
	claims, err := verifyAndParseJWT(tokenString, a.cfg)
	if err != nil {
		return nil, &AuthError{Code: http.StatusUnauthorized, Message: "invalid jwt"}
	}

	sub, _ := claims["sub"].(string)
	if sub == "" {
		return nil, &AuthError{Code: http.StatusUnauthorized, Message: "missing sub"}
	}

	email, _ := claims["email"].(string)
	rolesAny, _ := claims["roles"].([]interface{})

	var roles []string
	for _, rAny := range rolesAny {
		if s, ok := rAny.(string); ok {
			roles = append(roles, s)
		}
	}

	if len(roles) == 0 {
		return nil, &AuthError{Code: http.StatusForbidden, Message: "no roles assigned"}
	}

	return &Identity{
		UserID: sub,
		Email:  email,
		Roles:  roles,
		Source: "jwt",
	}, nil
}

// Stub conceptual: cambia por implementación real.
func verifyAndParseJWT(token string, cfg JWTConfig) (map[string]interface{}, error) {

	// TODO - parse & validate signature
	// TODO - validate iss, aud, exp, nbf...
	return map[string]interface{}{
		"sub":   "demo-user",
		"email": "demo@example.com",
		"roles": []interface{}{"admin"},
		"exp":   time.Now().Add(1 * time.Hour).Unix(),
	}, nil
}
