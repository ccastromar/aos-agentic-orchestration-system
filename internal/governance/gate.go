// Package governance is the enforcement point (PEP) for the Prudence governance vertex.
// At pipeline entry, AOS asks Prudence's Policy Decision Point (PDP) whether a subject may
// run a given pipeline against a declared AI system. AOS enforces the verdict; the policy
// lives in Prudence, not here.
//
// Opt-in and additive: the gate runs only when a pipeline declares `ai_system` AND a PDP
// URL is configured (PRUDENCE_PDP_URL); a pipeline marked `governed` additionally fails
// closed when either is missing (enforced by the planner). When it runs, it is fail-closed —
// an unreachable PDP yields a transport error the caller must treat as a denial. DENY and
// APPROVAL_REQUIRED both stop the task: approval happens in Prudence's review workflow,
// never through a local AOS gate.
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
	"github.com/ccastromar/aos-agentic-orchestration-system/internal/metrics"
	"net/http"
	"strings"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/telemetry"
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
	// DecisionID is the id Prudence recorded the decision under (Decision API v1); the
	// execution report cites it. Never empty: a decision without one is refused.
	DecisionID string
}

type decisionRequest struct {
	Subject    string `json:"subject"`
	Pipeline   string `json:"pipeline"`
	AiSystemID string `json:"aiSystemId"`
	Action     string `json:"action"`
	// CorrelationID is the task the decision belongs to (Decision API v1, optional).
	CorrelationID string `json:"correlationId,omitempty"`
}

type decisionResponse struct {
	Decision   string `json:"decision"`
	Reason     string `json:"reason"`
	Approver   string `json:"approver"`
	DecisionID string `json:"decisionId"`
}

// Evaluate consults the Prudence PDP at pdpURL. A pipeline that is not governed (pdpURL or
// aiSystem empty) is an implicit Allow. A transport error is returned so the caller can
// fail closed. A valid PDP response maps to a Decision.
func Evaluate(ctx context.Context, pdpURL, subject, pipeline, aiSystem, action string) (Decision, error) {
	if pdpURL == "" || aiSystem == "" {
		return Decision{Status: Allow}, nil // not governed
	}
	started := time.Now()
	dec, err := evaluate(ctx, pdpURL, subject, pipeline, aiSystem, action)
	verdict := dec.Status
	if err != nil {
		verdict = "ERROR" // fail closed: no verdict AOS may act on
	}
	metrics.GateDuration.Observe(map[string]string{"verdict": verdict}, time.Since(started).Seconds())
	return dec, err
}

func evaluate(ctx context.Context, pdpURL, subject, pipeline, aiSystem, action string) (Decision, error) {
	if action == "" {
		action = "run"
	}
	correlationID := CorrelationID(ctx)
	body, _ := json.Marshal(decisionRequest{Subject: subject, Pipeline: pipeline, AiSystemID: aiSystem, Action: action, CorrelationID: correlationID})

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, pdpURL+"/api/policy/decisions", bytes.NewReader(body))
	if err != nil {
		return Decision{}, fmt.Errorf("governance: cannot build PDP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if correlationID != "" {
		req.Header.Set(CorrelationHeader, correlationID)
	}

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

	resp, err := telemetry.HTTPClient.Do(req)
	if err != nil {
		return Decision{}, fmt.Errorf("governance: PDP unreachable (fail-closed): %w", err)
	}
	defer resp.Body.Close()

	var dr decisionResponse
	if decErr := json.NewDecoder(resp.Body).Decode(&dr); decErr != nil {
		return Decision{}, fmt.Errorf("governance: unreadable PDP response (status %d)", resp.StatusCode)
	}
	// The status and the decision come in pairs (Decision API contract, section 2): an ALLOW
	// under any status but 200, or a decision without the id it was recorded under, is not a
	// decision AOS may act on.
	if want, ok := decisionForStatus[resp.StatusCode]; !ok || dr.Decision != want {
		return Decision{}, fmt.Errorf("governance: unexpected PDP answer: status %d with decision %q", resp.StatusCode, dr.Decision)
	}
	if strings.TrimSpace(dr.DecisionID) == "" {
		return Decision{}, fmt.Errorf("governance: PDP decision %s without a decisionId", dr.Decision)
	}
	return Decision{Status: dr.Decision, Reason: dr.Reason, Approver: dr.Approver, DecisionID: dr.DecisionID}, nil
}

// decisionForStatus is the only decision each status may carry. 503 (ERROR: Prudence could
// not decide) is not listed: it is never a decision AOS acts on, so it stops as an error.
var decisionForStatus = map[int]string{
	http.StatusOK:        Allow,
	http.StatusAccepted:  ApprovalRequired,
	http.StatusForbidden: Deny,
}
