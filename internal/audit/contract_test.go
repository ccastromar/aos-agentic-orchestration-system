package audit

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AOS's side of the audit events contract (contract/audit-events): every journal record becomes
// exactly the Kafka record the vectors pin, and nothing sensitive leaves AOS. Ernest's
// event-ingestor runs the same vectors against its Kafka source.

var updateContract = flag.Bool("update-contract", false, "rewrite the records in contract/audit-events/vectors.json")

type contractRecord struct {
	Key     string            `json:"key"`
	Headers map[string]string `json:"headers"`
	Value   json.RawMessage   `json:"value"`
}

func TestAuditEventsContract(t *testing.T) {
	path := filepath.Join("..", "..", "contract", "audit-events", "vectors.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var forbidden []string
	for _, f := range doc["forbidden"].([]any) {
		forbidden = append(forbidden, f.(string))
	}
	for _, c := range doc["cases"].([]any) {
		c := c.(map[string]any)
		t.Run(c["name"].(string), func(t *testing.T) {
			journal, _ := json.Marshal(c["journal"])
			var rec Record
			if err := json.Unmarshal(journal, &rec); err != nil {
				t.Fatal(err)
			}
			kr, err := JournalRecord(rec, c["aiSystemId"].(string))
			if err != nil {
				t.Fatal(err)
			}
			got := contractRecord{Key: string(kr.Key), Headers: map[string]string{}, Value: kr.Value}
			for _, h := range kr.Headers {
				got.Headers[h.Key] = string(h.Value)
			}
			for _, f := range forbidden {
				if strings.Contains(string(kr.Value), f) || strings.Contains(string(kr.Key), f) {
					t.Errorf("%q leaves AOS: %s", f, kr.Value)
				}
			}
			if *updateContract {
				c["record"] = got
				return
			}
			want, _ := json.Marshal(c["record"])
			have, _ := json.Marshal(got)
			if !jsonEqual(t, want, have) {
				t.Errorf("record changed:\n got  %s\n want %s\nA change Ernest must see is a new contract version (README.md); otherwise run with -update-contract", have, want)
			}
		})
	}
	if *updateContract {
		out, _ := json.MarshalIndent(doc, "", "  ")
		if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func jsonEqual(t *testing.T, a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		t.Fatal("bad JSON")
	}
	xa, _ := json.Marshal(x)
	ya, _ := json.Marshal(y)
	return string(xa) == string(ya)
}
