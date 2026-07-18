package auth

import (
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// devJWTSecret signs tokens in local development when JWT_SECRET is not set,
// so the login flow works out of the box. Never rely on it outside dev.
const devJWTSecret = "aos-dev-secret"

func signingSecret() []byte {
	if s := os.Getenv("JWT_SECRET"); s != "" {
		return []byte(s)
	}
	return []byte(devJWTSecret)
}

// IssueToken creates a signed HS256 JWT for the given user.
//
// This is a development stub: credentials are NOT validated. The intended
// production path is delegating authentication to Keycloak (see AOS_JWK_URL /
// AOS_JWT_ISSUER in config) and dropping local issuance entirely — the rest
// of the system only consumes the resulting Identity, so nothing else changes.
func IssueToken(username string, roles []string, ttl time.Duration) (string, error) {
	if len(roles) == 0 {
		roles = []string{"user"}
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":   username,
		"email": username + "@local",
		"roles": roles,
		"iss":   "aos-dev-login",
		"iat":   now.Unix(),
		"exp":   now.Add(ttl).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(signingSecret())
}
