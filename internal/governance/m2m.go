package governance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// m2mAuth obtains an OAuth2 client-credentials token for calling a *secured* Prudence PDP
// (the `pdp.decide` scope) and caches it until shortly before expiry. The enforcement point
// authenticates as a machine client, mirroring the Python adapters' M2MAuth. Configured from
// the environment; when unset, the PDP is called without a token (an open/dev PDP).
type m2mAuth struct {
	tokenURL     string
	clientID     string
	clientSecret string
	scope        string

	mu    sync.Mutex
	token string
	exp   time.Time
}

var (
	authOnce sync.Once
	authInst *m2mAuth
)

// pdpAuthFromEnv returns the configured client-credentials auth, or nil when the PDP is not
// secured (any of the three vars unset). Read once per process.
func pdpAuthFromEnv() *m2mAuth {
	authOnce.Do(func() {
		tokenURL := strings.TrimSpace(os.Getenv("PRUDENCE_PDP_TOKEN_URL"))
		clientID := strings.TrimSpace(os.Getenv("PRUDENCE_PDP_CLIENT_ID"))
		clientSecret := strings.TrimSpace(os.Getenv("PRUDENCE_PDP_CLIENT_SECRET"))
		if tokenURL == "" || clientID == "" || clientSecret == "" {
			return
		}
		scope := strings.TrimSpace(os.Getenv("PRUDENCE_PDP_SCOPE"))
		if scope == "" {
			scope = "pdp.decide"
		}
		authInst = &m2mAuth{tokenURL: tokenURL, clientID: clientID, clientSecret: clientSecret, scope: scope}
	})
	return authInst
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// bearer returns a valid access token, fetching (and caching) a new one when needed.
func (a *m2mAuth) bearer(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.token != "" && time.Now().Before(a.exp.Add(-30*time.Second)) {
		return a.token, nil
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", a.clientID)
	form.Set("client_secret", a.clientSecret)
	form.Set("scope", a.scope)

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, a.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("token endpoint status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", err
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("token endpoint returned no access_token")
	}
	ttl := tr.ExpiresIn
	if ttl <= 0 {
		ttl = 300
	}
	a.token = tr.AccessToken
	a.exp = time.Now().Add(time.Duration(ttl) * time.Second)
	return a.token, nil
}
