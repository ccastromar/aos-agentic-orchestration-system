package agent

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	// maxFacts caps how many scalar values are kept per memory so a verbose
	// tool response cannot flood the vector store payload.
	maxFacts = 40
	// maxFactValueLen truncates individual values (e.g. long descriptions).
	maxFactValueLen = 200
)

// factNoise are bookkeeping keys added by the tool executor that carry no
// domain information worth remembering.
var factNoise = map[string]bool{
	"ok":         true,
	"statusCode": true,
}

// extractFacts flattens the scalar values of a pipeline's raw step results
// into dotted-path keys ("create_ticket.ticketId" -> "T-123"), so concrete
// data survives in long-term memory alongside the LLM summary.
func extractFacts(raw map[string]any) map[string]string {
	facts := make(map[string]string)
	flattenFacts("", raw, facts)
	return facts
}

func flattenFacts(prefix string, node map[string]any, out map[string]string) {
	for k, v := range node {
		if factNoise[k] {
			continue
		}
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		flattenFactValue(key, v, out)
	}
}

func flattenFactValue(key string, v any, out map[string]string) {
	switch val := v.(type) {
	case map[string]any:
		flattenFacts(key, val, out)
	case []any:
		for i, el := range val {
			flattenFactValue(key+"."+strconv.Itoa(i), el, out)
		}
	case string, bool, int, int64, float32, float64:
		if len(out) >= maxFacts {
			return
		}
		s := fmt.Sprintf("%v", val)
		if len(s) > maxFactValueLen {
			s = s[:maxFactValueLen]
		}
		out[key] = s
	}
}

// factsText renders facts as sorted "key: value" lines for embedding.
func factsText(facts map[string]string) string {
	if len(facts) == 0 {
		return ""
	}
	keys := make([]string, 0, len(facts))
	for k := range facts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString(": ")
		sb.WriteString(facts[k])
		sb.WriteString("\n")
	}
	return sb.String()
}
