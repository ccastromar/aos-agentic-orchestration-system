// Package governancedemo serves the read-only tools behind the Prudence-governed demo
// pipelines. Whether they are ever reached is Prudence's decision, not theirs.
package governancedemo

import (
	"encoding/json"
	"hash/fnv"
	"log"
	"net/http"
	"strings"
)

func RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/mock/governance-demo/credit/pre-screening", handleCreditPreScreening)
	mux.HandleFunc("/mock/governance-demo/social-score", handleSocialScore)
	mux.HandleFunc("/mock/governance-demo/translate", handleTranslate)
}

// handleCreditPreScreening prioritises an application for human review. It never decides
// eligibility: the output is a queue position for a credit officer.
func handleCreditPreScreening(w http.ResponseWriter, r *http.Request) {
	applicationID := r.URL.Query().Get("applicationId")
	if applicationID == "" {
		applicationID = "unknown"
	}
	priority := "standard"
	if stableBucket(applicationID, 3) == 0 {
		priority = "expedite"
	}
	writeJSON(w, map[string]any{
		"applicationId": applicationID,
		"reviewQueue":   "retail-credit-officers",
		"priority":      priority,
		"decision":      "pending human review",
	})
}

// handleSocialScore must never be reached: social scoring is a prohibited practice and
// Prudence denies the pipeline before AOS calls any tool. If it is reached, the governance
// gate is misconfigured — say so loudly instead of returning a score.
func handleSocialScore(w http.ResponseWriter, r *http.Request) {
	log.Printf("[MOCK] WARNING: social-score tool reached for citizenId=%q — the governance gate did not stop a PROHIBITED system", r.URL.Query().Get("citizenId"))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": "prohibited practice: this tool refuses to score people; it should never have been called",
	})
}

var glossary = map[string]map[string]string{
	"pt": {"the": "a", "branch": "agência", "opens": "abre", "at": "às", "nine": "nove", "la": "a", "oficina": "agência", "abre": "abre", "a": "às", "las": "", "nueve": "nove"},
	"en": {"la": "the", "oficina": "branch", "abre": "opens", "a": "at", "las": "", "nueve": "nine"},
	"es": {"the": "la", "branch": "oficina", "opens": "abre", "at": "a las", "nine": "nueve"},
}

// handleTranslate is a word-by-word glossary lookup, enough to show a result on screen.
func handleTranslate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text           string `json:"text"`
		TargetLanguage string `json:"targetLanguage"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	target := languageCode(req.TargetLanguage)
	words := glossary[target]
	var out []string
	for _, word := range strings.Fields(req.Text) {
		key := strings.ToLower(strings.Trim(word, ".,;:!?\"'"))
		if tr, ok := words[key]; ok {
			if tr != "" {
				out = append(out, tr)
			}
			continue
		}
		out = append(out, word)
	}
	writeJSON(w, map[string]any{
		"targetLanguage": target,
		"translation":    strings.Join(out, " "),
		"method":         "demo glossary",
	})
}

func languageCode(name string) string {
	switch n := strings.ToLower(strings.TrimSpace(name)); {
	case strings.HasPrefix(n, "port"), n == "pt":
		return "pt"
	case strings.HasPrefix(n, "ingl"), strings.HasPrefix(n, "engl"), n == "en":
		return "en"
	case strings.HasPrefix(n, "espa"), strings.HasPrefix(n, "span"), n == "es":
		return "es"
	default:
		return n
	}
}

func stableBucket(s string, n uint32) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32() % n
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}
