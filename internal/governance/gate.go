// Package governance is the enforcement point (PEP) for the Prudence governance vertex.
// At pipeline entry, AOS asks Prudence's Policy Decision Point (PDP) whether a subject may
// run a given pipeline against a declared AI system. AOS enforces the verdict; the policy
// lives in Prudence, not here.
//
// Opt-in and additive: the gate runs only when a pipeline declares `ai_system` AND a PDP
// URL is configured (PRUDENCE_PDP_URL). When it runs, it is fail-closed — an unreachable
// PDP yields a transport error the caller must treat as a denial. An APPROVAL_REQUIRED
// verdict is routed into AOS's existing human-approval gate rather than failing outright.
//
// Against a secured PDP, the gate authenticates with an OAuth2 client-credentials token
// (the `pdp.decide` scope) when PRUDENCE_PDP_TOKEN_URL / _CLIENT_ID / _CLIENT_SECRET are set
// (see m2m.go). Obtaining the token is itself fail-closed.
package governance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Verdict kinds returned by the Prudence PDP.
const (
	Allow            = "ALLOW"
	Deny             = "DENY"
	ApprovalRequired = "APPROVAL_REQUIRED"
)

// Decision is the PDP verdict for a pipeline-entry check.
type Decision struct {
	Status   string // Allow | Deny | ApprovalRequired
	Reason   string
	Approver string
}

type decisionRequest struct {
	Subject    string `json:"subject"`
	Pipeline   string `json:"pipeline"`
	AiSystemID string `json:"aiSystemId"`
	Action     string `json:"action"`
}

type decisionResponse struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	Approver string `json:"approver"`
}

// Evaluate consults the Prudence PDP at pdpURL. A pipeline that is not governed (pdpURL or
// aiSystem empty) is an implicit Allow. A transport error is returned so the caller can
// fail closed. A valid PDP response maps to a Decision.
func Evaluate(ctx context.Context, pdpURL, subject, pipeline, aiSystem, action string) (Decision, error) {
	if pdpURL == "" || aiSystem == "" {
		return Decision{Status: Allow}, nil // not governed
	}
	if action == "" {
		action = "run"
	}
	body, _ := json.Marshal(decisionRequest{Subject: subject, Pipeline: pipeline, AiSystemID: aiSystem, Action: action})

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, pdpURL+"/api/policy/decisions", bytes.NewReader(body))
	if err != nil {
		return Decision{}, fmt.Errorf("governance: cannot build PDP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// When the PDP is secured (PRUDENCE_PDP_* set), authenticate as a machine client with a
	// client-credentials token carrying the `pdp.decide` scope. Fail closed if it can't be
	// obtained — no token means no authority to run.
	if auth := pdpAuthFromEnv(); auth != nil {
		tok, tokErr := auth.bearer(reqCtx)
		if tokErr != nil {
			return Decision{}, fmt.Errorf("governance: cannot obtain PDP token (fail-closed): %w", tokErr)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Decision{}, fmt.Errorf("governance: PDP unreachable (fail-closed): %w", err)
	}
	defer resp.Body.Close()

	var dr decisionResponse
	if decErr := json.NewDecoder(resp.Body).Decode(&dr); decErr != nil {
		return Decision{}, fmt.Errorf("governance: unreadable PDP response (status %d)", resp.StatusCode)
	}
	switch dr.Decision {
	case Allow, Deny, ApprovalRequired:
		return Decision{Status: dr.Decision, Reason: dr.Reason, Approver: dr.Approver}, nil
	default:
		return Decision{}, fmt.Errorf("governance: unexpected PDP decision %q", dr.Decision)
	}
}
