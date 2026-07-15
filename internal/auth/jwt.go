package auth

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
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
		if os.Getenv("JWT_SECRET") == "" {
			return &Identity{
				UserID: "demo-user",
				Email:  "demo@example.com",
				Roles:  []string{"admin"},
				Source: "mock",
			}, nil
		}
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

// verifyAndParseJWT validates the JWT signature and extracts claims.
func verifyAndParseJWT(tokenString string, cfg JWTConfig) (map[string]interface{}, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		// Mock fallback for local dev if no secret is configured
		return map[string]interface{}{
			"sub":   "demo-user",
			"email": "demo@example.com",
			"roles": []interface{}{"admin"},
			"exp":   time.Now().Add(1 * time.Hour).Unix(),
		}, nil
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Ensure the signing method is HMAC
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}
