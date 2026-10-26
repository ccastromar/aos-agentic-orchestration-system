package governance

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// AOS's gate against the Decision API contract (decision-contract/, synced from
// prudence-governance contract/decision): every vector's answer, and the shape of what AOS sends.

func contractJSON(t *testing.T, name string, v any) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("decision-contract", "vectors", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatal(err)
	}
}

func noPDPToken(t *testing.T) {
	for _, k := range []string{"PRUDENCE_PDP_TOKEN_URL", "PRUDENCE_PDP_CLIENT_ID", "PRUDENCE_PDP_CLIENT_SECRET"} {
		t.Setenv(k, "")
	}
}

func answering(t *testing.T, status int, body string, seen *map[string]any) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			data, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(data, seen)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDecisionContractGateVectors(t *testing.T) {
	noPDPToken(t)
	var vectors struct {
		Cases []struct {
			Name   string `json:"name"`
			Status int    `json:"status"`
			Body   string `json:"body"`
			Expect struct {
				Run        bool    `json:"run"`
				DecisionID *string `json:"decisionId"`
				Decision   *string `json:"decision"`
				Approver   *string `json:"approver"`
			} `json:"expect"`
		} `json:"cases"`
	}
	contractJSON(t, "gate.json", &vectors)
	for _, c := range vectors.Cases {
		t.Run(c.Name, func(t *testing.T) {
			srv := answering(t, c.Status, c.Body, nil)
			d, err := Evaluate(context.Background(), srv.URL, "aos", "credit_prescreening", "sys-1", "run")
			if run := err == nil && d.Status == Allow; run != c.Expect.Run {
				t.Fatalf("run = %v, want %v (decision %+v, err %v)", run, c.Expect.Run, d, err)
			}
			if c.Expect.DecisionID != nil && d.DecisionID != *c.Expect.DecisionID {
				t.Errorf("decisionId = %q, want %q", d.DecisionID, *c.Expect.DecisionID)
			}
			if c.Expect.Decision != nil && (err != nil || d.Status != *c.Expect.Decision) {
				t.Errorf("decision = %q (err %v), want %q", d.Status, err, *c.Expect.Decision)
			}
			if c.Expect.Approver != nil && d.Approver != *c.Expect.Approver {
				t.Errorf("approver = %q, want %q", d.Approver, *c.Expect.Approver)
			}
		})
	}
}

func TestDecisionContractReportVectors(t *testing.T) {
	noPDPToken(t)
	var vectors struct {
		Cases []struct {
			Status int    `json:"status"`
			Expect string `json:"expect"`
		} `json:"cases"`
	}
	contractJSON(t, "report.json", &vectors)
	for _, c := range vectors.Cases {
		t.Run(http.StatusText(c.Status), func(t *testing.T) {
			srv := answering(t, c.Status, "{}", nil)
			retry, err := reportExecution(context.Background(), srv.URL, sampleReport())
			got := "recorded"
			if err != nil {
				got = "drop"
				if retry {
					got = "retry"
				}
			}
			if got != c.Expect {
				t.Fatalf("%d: %s, want %s", c.Status, got, c.Expect)
			}
		})
	}
}

func sampleReport() ExecutionReport {
	return ExecutionReport{
		Runtime: "aos", ExecutionID: "trace-1", DecisionID: "d-1", AiSystemID: "sys-1",
		Pipeline: "credit_prescreening", StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// What AOS sends matches the OpenAPI schemas: every required field, nothing undeclared.
func TestDecisionContractRequestShapes(t *testing.T) {
	noPDPToken(t)
	data, err := os.ReadFile(filepath.Join("decision-contract", "decision-api-v1.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Required   []string       `yaml:"required"`
				Properties map[string]any `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	check := func(schema string, sent map[string]any) {
		s := spec.Components.Schemas[schema]
		for _, k := range s.Required {
			if _, ok := sent[k]; !ok {
				t.Errorf("%s: AOS does not send required %s", schema, k)
			}
		}
		for k, v := range sent {
			if _, ok := s.Properties[k]; !ok {
				t.Errorf("%s: AOS sends %s, which the schema does not declare", schema, k)
			}
			if _, ok := v.(string); !ok {
				t.Errorf("%s: %s is %T, not a string", schema, k, v)
			}
		}
	}

	var decide map[string]any
	srv := answering(t, 200, `{"decision":"ALLOW","decisionId":"d-1"}`, &decide)
	if _, err := Evaluate(context.Background(), srv.URL, "aos", "credit_prescreening", "sys-1", ""); err != nil {
		t.Fatal(err)
	}
	check("DecisionRequest", decide)
	if decide["action"] != "run" {
		t.Errorf("default action = %v, want run", decide["action"])
	}

	var report map[string]any
	srv = answering(t, 201, "{}", &report)
	if _, err := reportExecution(context.Background(), srv.URL, sampleReport()); err != nil {
		t.Fatal(err)
	}
	check("ExecutionReport", report)
	if _, err := time.Parse(time.RFC3339Nano, report["startedAt"].(string)); err != nil {
		t.Errorf("startedAt %v is not an ISO-8601 instant", report["startedAt"])
	}
}
