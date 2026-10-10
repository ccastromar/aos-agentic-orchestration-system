// Package governance is the enforcement point (PEP) for the Prudence governance vertex.
// At pipeline entry, AOS asks Prudence's Policy Decision Point (PDP) whether a subject may
// run a given pipeline against a declared AI system. AOS enforces the verdict; the policy
// lives in Prudence, not here.
//
// Opt-in and additive: the gate runs only when a pipeline declares `ai_system` AND a PDP
// URL is configured (PRUDENCE_PDP_URL). When it runs, it is fail-closed — an unreachable or
// non-allowing PDP blocks the run.
package governance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type decisionRequest struct {
	Subject    string `json:"subject"`
	Pipeline   string `json:"pipeline"`
	AiSystemID string `json:"aiSystemId"`
	Action     string `json:"action"`
}

type decisionResponse struct {
	Decision string `json:"decision"` // ALLOW | DENY | APPROVAL_REQUIRED
	Reason   string `json:"reason"`
	Approver string `json:"approver"`
}

// Check consults the Prudence PDP at pdpURL and returns nil only when the decision is ALLOW.
// It is a no-op (returns nil) when the pipeline is not governed: pdpURL or aiSystem empty.
func Check(ctx context.Context, pdpURL, subject, pipeline, aiSystem, action string) error {
	if pdpURL == "" || aiSystem == "" {
		return nil // pipeline not governed; nothing to enforce
	}
	if action == "" {
		action = "run"
	}
	body, _ := json.Marshal(decisionRequest{Subject: subject, Pipeline: pipeline, AiSystemID: aiSystem, Action: action})

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, pdpURL+"/api/policy/decisions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("governance: cannot build PDP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Fail-closed: a governed pipeline must not run when the PDP is unreachable.
		return fmt.Errorf("governance: PDP unreachable, denying (fail-closed): %w", err)
	}
	defer resp.Body.Close()

	var dr decisionResponse
	if decErr := json.NewDecoder(resp.Body).Decode(&dr); decErr != nil {
		return fmt.Errorf("governance: unreadable PDP response (status %d), denying", resp.StatusCode)
	}

	switch dr.Decision {
	case "ALLOW":
		return nil
	case "APPROVAL_REQUIRED":
		return fmt.Errorf("governance: approval required before running %q (approver: %s): %s", pipeline, dr.Approver, dr.Reason)
	case "DENY":
		return fmt.Errorf("governance: denied running %q: %s", pipeline, dr.Reason)
	default:
		return fmt.Errorf("governance: unexpected PDP decision %q for %q, denying", dr.Decision, pipeline)
	}
}
