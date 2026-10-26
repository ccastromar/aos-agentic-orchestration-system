package governance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ccastromar/aos-agentic-orchestration-system/internal/logx"
)

// ExecutionReport is what AOS tells Prudence once a governed pipeline has really started
// (Decision API v1, POST /api/executions): which AI system ran, which pipeline, when, and the
// decision that allowed it. Prudence uses it as an execution source for drift.
type ExecutionReport struct {
	Runtime     string `json:"runtime"`
	ExecutionID string `json:"executionId"`
	DecisionID  string `json:"decisionId,omitempty"`
	AiSystemID  string `json:"aiSystemId"`
	Pipeline    string `json:"pipeline"`
	StartedAt   string `json:"startedAt"`
}

// reportAttempts and reportBackoff bound the retries of one report. Reporting happens after
// the pipeline has started, so it never blocks or fails a run: the same execution also
// reaches Ernest through the audit journal, and a report that is finally lost is logged.
var (
	reportAttempts = 5
	reportBackoff  = 2 * time.Second
)

// ReportsEnabled says whether AOS reports governed executions to Prudence: whenever a PDP is
// configured, unless PRUDENCE_REPORT_EXECUTIONS=false (for a Prudence without the endpoint).
func ReportsEnabled() bool {
	if strings.TrimSpace(os.Getenv("PRUDENCE_PDP_URL")) == "" {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(os.Getenv("PRUDENCE_REPORT_EXECUTIONS")), "false")
}

// ReportExecutionAsync sends the report in the background, retrying transient failures.
func ReportExecutionAsync(r ExecutionReport) {
	pdpURL := strings.TrimSpace(os.Getenv("PRUDENCE_PDP_URL"))
	if pdpURL == "" {
		return
	}
	go func() {
		wait := reportBackoff
		var err error
		for attempt := 1; attempt <= reportAttempts; attempt++ {
			var retry bool
			retry, err = reportExecution(context.Background(), pdpURL, r)
			if err == nil {
				return
			}
			if !retry {
				break
			}
			time.Sleep(wait)
			wait *= 2
		}
		logx.Warn("Governance", "execution %s of %s not reported to Prudence: %v", r.ExecutionID, r.AiSystemID, err)
	}()
}

// reportExecution posts one report. It says whether a failure is worth retrying: network
// errors and 5xx are; a 4xx (bad report, missing scope, conflicting facts) is not.
func reportExecution(ctx context.Context, pdpURL string, r ExecutionReport) (bool, error) {
	body, _ := json.Marshal(r)
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, pdpURL+"/api/executions", bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	if tp := traceparent(r.ExecutionID); tp != "" {
		req.Header.Set("traceparent", tp)
	}
	// The same machine identity as the gate: its token carries `execution.report` as a
	// default client scope alongside `pdp.decide`.
	if auth := pdpAuthFromEnv(); auth != nil {
		tok, tokErr := auth.bearer(reqCtx)
		if tokErr != nil {
			return true, fmt.Errorf("cannot obtain token: %w", tokErr)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return true, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		return false, nil
	}
	detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return resp.StatusCode >= 500, fmt.Errorf("Prudence answered %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
}
