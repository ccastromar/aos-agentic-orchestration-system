package auth

import (
	"encoding/json"
	"net/http"
	"time"
)

type APIKeyConfig struct {
	ResolveURL string
	Timeout    time.Duration
}

type APIKeyAuthenticator struct {
	cfg    APIKeyConfig
	client *http.Client
}

func NewAPIKeyAuthenticator(cfg APIKeyConfig) *APIKeyAuthenticator {
	return &APIKeyAuthenticator{
		cfg: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

func (a *APIKeyAuthenticator) Authenticate(r *http.Request) (*Identity, error) {
	apiKey := r.Header.Get("X-API-Key")
	if apiKey == "" {
		return nil, &AuthError{Code: http.StatusUnauthorized, Message: "missing api key"}
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, a.cfg.ResolveURL, nil)
	if err != nil {
		return nil, &AuthError{Code: http.StatusServiceUnavailable, Message: "iam request error"}
	}

	q := req.URL.Query()
	q.Set("api_key", apiKey)
	req.URL.RawQuery = q.Encode()

	// Opcional: auth de servicio hacia IAM
	// req.Header.Set("Authorization", "Bearer "+os.Getenv("AOS_IAM_TOKEN"))

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, &AuthError{Code: http.StatusServiceUnavailable, Message: "iam unreachable"}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &AuthError{Code: http.StatusUnauthorized, Message: "invalid api key"}
	}

	var body struct {
		UserID string   `json:"user_id"`
		Roles  []string `json:"roles"`
		Email  string   `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, &AuthError{Code: http.StatusServiceUnavailable, Message: "invalid iam response"}
	}

	if body.UserID == "" {
		return nil, &AuthError{Code: http.StatusUnauthorized, Message: "missing user_id from iam"}
	}
	if len(body.Roles) == 0 {
		return nil, &AuthError{Code: http.StatusForbidden, Message: "no roles for api key"}
	}

	return &Identity{
		UserID: body.UserID,
		Roles:  body.Roles,
		Email:  body.Email,
		Source: "apikey",
	}, nil
}
