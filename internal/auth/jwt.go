package auth

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/governance"
	"github.com/golang-jwt/jwt/v5"
)

type JWTConfig struct {
	Issuer   string
	Audience string
	JWKURL   string // OIDC
	// RoleClaim is the (dotted) claim holding the roles; RoleMap translates the identity
	// provider's role names into AOS roles. Empty values keep the defaults.
	RoleClaim string
	RoleMap   map[string]string
	// AllowDevFallback enables the mock identity and signing key for local tests.
	AllowDevFallback bool
	// Governance resolves the token's governance functions in AOS; nil resolves none.
	Governance *governance.Model
}

// IsDevelopmentEnvironment reports whether mock authentication is acceptable.
func IsDevelopmentEnvironment(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "dev", "development", "local", "test":
		return true
	default:
		return false
	}
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
		if os.Getenv("JWT_SECRET") == "" && a.cfg.AllowDevFallback {
			mockRole := r.Header.Get("X-Mock-Role")
			if mockRole == "" {
				mockRole = "admin"
			}
			return &Identity{
				UserID: "demo-user",
				Email:  "demo@example.com",
				Roles:  []string{mockRole},
				// The development mock is not an IdP token: its role implies its functions.
				Functions: LegacyFunctions([]string{mockRole}),
				Source:    "mock",
			}, nil
		}
		if os.Getenv("JWT_SECRET") == "" {
			return nil, &AuthError{Code: http.StatusUnauthorized, Message: "JWT_SECRET is required outside development mode"}
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
	roles := extractRoles(claims, a.cfg.RoleClaim, a.cfg.RoleMap)

	// A token from the identity provider (JWKS configured) gets its governance functions from
	// the model; a development token signed with JWT_SECRET from its AOS roles.
	var functions []string
	if a.cfg.JWKURL != "" && a.cfg.Governance != nil {
		functions = a.cfg.Governance.Resolve(claims, "aos")
	} else {
		functions = LegacyFunctions(roles)
	}
	if len(functions) == 0 {
		return nil, &AuthError{Code: http.StatusForbidden, Message: "no governance functions assigned"}
	}

	return &Identity{
		UserID:    sub,
		Email:     email,
		Roles:     roles,
		Source:    "jwt",
		Functions: functions,
	}, nil
}

// verifyAndParseJWT validates the JWT signature and extracts claims. Local
// mock-token fallback is enabled only when explicitly configured.
func verifyAndParseJWT(tokenString string, cfg JWTConfig) (map[string]interface{}, error) {
	// OIDC path (shared Chestertown IdP): when a JWKS URL is configured, validate RS256 against the
	// realm's published keys and the expected issuer. Keycloak tokens carry sub/email/roles.
	if cfg.JWKURL != "" {
		opts := []jwt.ParserOption{jwt.WithValidMethods([]string{"RS256"})}
		if cfg.Issuer != "" {
			opts = append(opts, jwt.WithIssuer(cfg.Issuer))
		}
		oidcToken, err := jwt.Parse(tokenString, rsaKeyfunc(cfg.JWKURL), opts...)
		if err != nil {
			return nil, fmt.Errorf("invalid oidc token: %w", err)
		}
		if claims, ok := oidcToken.Claims.(jwt.MapClaims); ok && oidcToken.Valid {
			return claims, nil
		}
		return nil, fmt.Errorf("invalid oidc token")
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Ensure the signing method is HMAC
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return signingSecret(), nil
	})

	if err == nil && token.Valid {
		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			return claims, nil
		}
	}

	if os.Getenv("JWT_SECRET") == "" && cfg.AllowDevFallback {
		// Mock fallback for local dev if no secret is configured
		return map[string]interface{}{
			"sub":   "demo-user",
			"email": "demo@example.com",
			"roles": []interface{}{"admin"},
			"exp":   time.Now().Add(1 * time.Hour).Unix(),
		}, nil
	}
	if os.Getenv("JWT_SECRET") == "" {
		return nil, fmt.Errorf("JWT_SECRET is required outside development mode")
	}

	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("invalid token")
}
