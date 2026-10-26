package governance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pdpStub answers /api/policy/decisions with a fixed body and records the last request.
func pdpStub(t *testing.T, status int, body string) (*httptest.Server, *decisionRequest, *atomic.Int32) {
	t.Helper()
	var got decisionRequest
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// assert, not require: this runs on the server goroutine, where FailNow must not be called.
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/policy/decisions", r.URL.Path)
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &got, &calls
}

func TestEvaluateUngovernedIsImplicitAllowWithoutCallingThePDP(t *testing.T) {
	srv, _, calls := pdpStub(t, http.StatusOK, `{"decision":"DENY"}`)

	dec, err := Evaluate(context.Background(), "", "alice", "p", "sys-1", "run")
	require.NoError(t, err)
	require.Equal(t, Allow, dec.Status)

	dec, err = Evaluate(context.Background(), srv.URL, "alice", "p", "", "run")
	require.NoError(t, err)
	require.Equal(t, Allow, dec.Status)
	require.Zero(t, calls.Load(), "an ungoverned pipeline must not reach the PDP")
}

func TestEvaluateMapsEachVerdict(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   Decision
	}{
		{http.StatusOK, `{"decision":"ALLOW","decisionId":"d-1"}`, Decision{Status: Allow, DecisionID: "d-1"}},
		{http.StatusForbidden, `{"decision":"DENY","reason":"prohibited AI system","decisionId":"d-2"}`,
			Decision{Status: Deny, Reason: "prohibited AI system", DecisionID: "d-2"}},
		{http.StatusAccepted, `{"decision":"APPROVAL_REQUIRED","reason":"high-risk system awaiting governance approval","approver":"risk-officer","decisionId":"d-3"}`,
			Decision{Status: ApprovalRequired, Reason: "high-risk system awaiting governance approval", Approver: "risk-officer", DecisionID: "d-3"}},
	}
	for _, tc := range cases {
		t.Run(tc.want.Status, func(t *testing.T) {
			srv, _, _ := pdpStub(t, tc.status, tc.body)
			dec, err := Evaluate(context.Background(), srv.URL, "alice", "pipeline_x", "sys-1", "run")
			require.NoError(t, err)
			require.Equal(t, tc.want, dec)
		})
	}
}

func TestEvaluateSendsTheDecisionContextAndDefaultsTheAction(t *testing.T) {
	srv, got, _ := pdpStub(t, http.StatusOK, `{"decision":"ALLOW","decisionId":"d-1"}`)

	_, err := Evaluate(context.Background(), srv.URL, "service-account-aos-pdp", "pipeline_aos_credit_pre_screening", "sys-credit", "")
	require.NoError(t, err)
	require.Equal(t, decisionRequest{
		Subject:    "service-account-aos-pdp",
		Pipeline:   "pipeline_aos_credit_pre_screening",
		AiSystemID: "sys-credit",
		Action:     "run",
	}, *got)
}

func TestEvaluateFailsClosed(t *testing.T) {
	t.Run("unexpected decision", func(t *testing.T) {
		srv, _, _ := pdpStub(t, http.StatusOK, `{"decision":"MAYBE","decisionId":"d-1"}`)
		_, err := Evaluate(context.Background(), srv.URL, "alice", "p", "sys-1", "run")
		require.ErrorContains(t, err, "unexpected PDP answer")
	})
	t.Run("ALLOW under an error status", func(t *testing.T) {
		srv, _, _ := pdpStub(t, http.StatusInternalServerError, `{"decision":"ALLOW","decisionId":"d-1"}`)
		_, err := Evaluate(context.Background(), srv.URL, "alice", "p", "sys-1", "run")
		require.ErrorContains(t, err, "status 500 with decision")
	})
	t.Run("unreadable response", func(t *testing.T) {
		srv, _, _ := pdpStub(t, http.StatusInternalServerError, `<html>oops</html>`)
		_, err := Evaluate(context.Background(), srv.URL, "alice", "p", "sys-1", "run")
		require.ErrorContains(t, err, "unreadable PDP response")
	})
	t.Run("unreachable PDP", func(t *testing.T) {
		srv, _, _ := pdpStub(t, http.StatusOK, `{"decision":"ALLOW"}`)
		url := srv.URL
		srv.Close()
		_, err := Evaluate(context.Background(), url, "alice", "p", "sys-1", "run")
		require.ErrorContains(t, err, "PDP unreachable (fail-closed)")
	})
}
