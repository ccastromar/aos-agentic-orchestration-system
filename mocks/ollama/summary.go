package ollama

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type stepOutcome struct {
	name     string
	executed bool
	err      string
	fields   []string // "key=value" for scalar outputs, sorted
}

// summarize answers the analyst prompt. It follows the prompt's strict rules literally:
// a step with executed=false (or an error) means the operation was NOT completed, and only
// values present in the raw JSON are mentioned.
func summarize(prompt string, scenarios []Scenario) string {
	lang := "en"
	if strings.Contains(prompt, markerSummaryES) {
		lang = "es"
	}
	intent := quotedAfter(prompt, "propósito: \"")
	if intent == "" {
		intent = quotedAfter(prompt, "intent: \"")
	}
	rawEnd := markerSummaryRawEN
	if lang == "es" {
		rawEnd = markerSummaryRawEnd
	}
	rawJSON, _ := between(prompt, markerSummaryRaw, rawEnd)
	var raw map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(rawJSON)), &raw)

	steps := outcomes(raw)
	completed := len(steps) > 0
	var failed []stepOutcome
	for _, st := range steps {
		if !st.executed || st.err != "" {
			completed = false
			failed = append(failed, st)
		}
	}

	if completed {
		if pinned := scenarioSummary(scenarios, intent, lang); pinned != "" {
			return pinned
		}
	}
	return genericSummary(lang, intent, steps, failed, completed)
}

func outcomes(raw map[string]any) []stepOutcome {
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []stepOutcome
	for _, name := range names {
		m, ok := raw[name].(map[string]any)
		if !ok {
			continue
		}
		executed, hasFlag := m["executed"].(bool)
		if !hasFlag {
			continue
		}
		st := stepOutcome{name: name, executed: executed}
		if e, ok := m["error"]; ok && e != nil && fmt.Sprint(e) != "" {
			st.err = fmt.Sprint(e)
		}
		st.fields = scalarFields(m, 4)
		out = append(out, st)
	}
	return out
}

// scalarFields lists up to max scalar outputs of a step, looking one level into the common
// "result"/"data"/"response" wrappers.
func scalarFields(m map[string]any, max int) []string {
	source := m
	for _, wrapper := range []string{"result", "data", "response", "body"} {
		if inner, ok := m[wrapper].(map[string]any); ok {
			source = inner
			break
		}
	}
	keys := make([]string, 0, len(source))
	for k := range source {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		// Transport bookkeeping added by the tool executor is not business data.
		if k == "executed" || k == "error" || k == "ok" || k == "statusCode" || strings.HasPrefix(k, "_") {
			continue
		}
		switch v := source[k].(type) {
		case string, float64, bool:
			out = append(out, fmt.Sprintf("%s=%v", k, v))
		}
		if len(out) == max {
			break
		}
	}
	return out
}

func genericSummary(lang, intent string, steps, failed []stepOutcome, completed bool) string {
	names := func(list []stepOutcome) string {
		parts := make([]string, len(list))
		for i, st := range list {
			parts[i] = st.name
		}
		return strings.Join(parts, ", ")
	}
	var data []string
	for _, st := range steps {
		data = append(data, st.fields...)
	}
	var b strings.Builder
	if lang == "es" {
		switch {
		case len(steps) == 0:
			fmt.Fprintf(&b, "La operación «%s» terminó, pero el resultado no incluye pasos verificables, así que no puedo confirmar que se completara.", intent)
		case completed:
			fmt.Fprintf(&b, "La operación «%s» se completó: se ejecutaron %d paso(s) (%s).", intent, len(steps), names(steps))
			if len(data) > 0 {
				fmt.Fprintf(&b, " Datos devueltos: %s.", strings.Join(data, ", "))
			}
		default:
			fmt.Fprintf(&b, "La operación «%s» NO se completó. Pasos no ejecutados o con error: %s.", intent, names(failed))
			if e := firstError(failed); e != "" {
				fmt.Fprintf(&b, " Motivo: %s.", e)
			}
		}
		b.WriteString(" (Resumen del modelo de demostración determinista.)")
		return b.String()
	}
	switch {
	case len(steps) == 0:
		fmt.Fprintf(&b, "The %q operation finished, but the result has no verifiable steps, so I cannot confirm it was completed.", intent)
	case completed:
		fmt.Fprintf(&b, "The %q operation was completed: %d step(s) executed (%s).", intent, len(steps), names(steps))
		if len(data) > 0 {
			fmt.Fprintf(&b, " Returned data: %s.", strings.Join(data, ", "))
		}
	default:
		fmt.Fprintf(&b, "The %q operation was NOT completed. Steps not executed or failed: %s.", intent, names(failed))
		if e := firstError(failed); e != "" {
			fmt.Fprintf(&b, " Reason: %s.", e)
		}
	}
	b.WriteString(" (Summary by the deterministic demo model.)")
	return b.String()
}

func firstError(list []stepOutcome) string {
	for _, st := range list {
		if st.err != "" {
			return st.err
		}
	}
	return ""
}

func quotedAfter(s, prefix string) string {
	i := strings.Index(s, prefix)
	if i < 0 {
		return ""
	}
	rest := s[i+len(prefix):]
	j := strings.Index(rest, "\"")
	if j < 0 {
		return ""
	}
	return rest[:j]
}
