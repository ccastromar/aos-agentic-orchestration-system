package ollama

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var stopwords = map[string]bool{
	// en
	"the": true, "and": true, "for": true, "with": true, "this": true, "that": true, "from": true,
	"you": true, "your": true, "please": true, "can": true, "run": true, "what": true, "are": true,
	"demo": true, "now": true, "want": true, "need": true, "all": true, "into": true, "about": true,
	// es
	"los": true, "las": true, "del": true, "por": true, "para": true, "con": true, "una": true,
	"uno": true, "que": true, "mis": true, "mas": true, "este": true, "esta": true, "esto": true,
	"como": true, "quiero": true, "puedes": true, "favor": true, "ahora": true, "sobre": true,
}

// tokens normalizes text (lowercase, accents stripped) and returns meaningful words.
func tokens(text string) []string {
	text = stripAccents(strings.ToLower(text))
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) < 3 || stopwords[f] {
			continue
		}
		out = append(out, stem(f))
	}
	return out
}

// stem is a crude suffix trim so "transfers"/"transferencia"/"transferir" overlap.
func stem(w string) string {
	if len(w) > 6 {
		return w[:6]
	}
	return w
}

var accentFolder = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
	"à", "a", "è", "e", "ì", "i", "ò", "o", "ù", "u", "ç", "c", "ã", "a", "õ", "o", "â", "a", "ê", "e", "ô", "o",
)

// stripAccents folds the accented letters of the demo languages (es/pt/en) to ASCII.
func stripAccents(s string) string {
	return accentFolder.Replace(s)
}

func tokenSet(text string) map[string]bool {
	set := map[string]bool{}
	for _, t := range tokens(text) {
		set[t] = true
	}
	return set
}

// bestIntent scores each catalog intent by word overlap with the user message. Examples
// weigh double (they are positive matches by contract), the description and the intent
// name once. Ties break alphabetically so the answer is stable across runs.
func bestIntent(msg string, catalog map[string]catalogEntry) (string, float64) {
	msgSet := tokenSet(msg)
	if len(msgSet) == 0 {
		return "", 0
	}
	names := make([]string, 0, len(catalog))
	for name := range catalog {
		names = append(names, name)
	}
	sort.Strings(names)

	best, bestScore := "", 0.0
	for _, name := range names {
		entry := catalog[name]
		score := 0.0
		for _, ex := range entry.Examples {
			if strings.EqualFold(strings.TrimSpace(ex), strings.TrimSpace(msg)) {
				score += 100
			}
			score += 2 * overlap(msgSet, tokenSet(ex))
		}
		score += overlap(msgSet, tokenSet(entry.Description))
		score += overlap(msgSet, tokenSet(strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(name)))
		if score > bestScore {
			best, bestScore = name, score
		}
	}
	// One shared word is noise; require at least two weighted hits.
	if bestScore < 2 {
		return "", 0
	}
	confidence := 0.55 + bestScore/40
	if confidence > 0.95 {
		confidence = 0.95
	}
	return best, confidence
}

func overlap(a, b map[string]bool) float64 {
	n := 0.0
	for t := range b {
		if a[t] {
			n++
		}
	}
	return n
}

var (
	reEmail  = regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.-]+`)
	reIBAN   = regexp.MustCompile(`\b[A-Z]{2}\d{2}[A-Z0-9]{10,30}\b`)
	reAmount = regexp.MustCompile(`\b\d+(?:[.,]\d{1,2})?\b`)
	reIDLike = regexp.MustCompile(`\b[A-Za-z]*[-_]?\d[\w-]*\b`)
	reDate   = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`)
	reQuoted = regexp.MustCompile(`"([^"]+)"|'([^']+)'|«([^»]+)»`)

	reServiceEN = regexp.MustCompile(`(?i)\b([\w-]+)\s+service\b`)
	reServiceES = regexp.MustCompile(`(?i)\bservicio\s+(?:de\s+)?([\w-]+)`)
)

// guessParam is a small, explainable extractor: an explicit "name: value" / "name=value"
// wins; otherwise the parameter name hints at a shape (email, iban, amount, date, id).
func guessParam(msg, name string) string {
	if msg == "" {
		return ""
	}
	explicit := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\b\s*[:=]\s*("[^"]+"|[^\s,;]+)`)
	if m := explicit.FindStringSubmatch(msg); m != nil {
		return strings.Trim(m[1], `"`)
	}
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "email") || strings.Contains(lower, "mail"):
		return reEmail.FindString(msg)
	case strings.Contains(lower, "iban"):
		return reIBAN.FindString(msg)
	case strings.Contains(lower, "date") || strings.Contains(lower, "fecha"):
		return reDate.FindString(msg)
	case strings.Contains(lower, "amount") || strings.Contains(lower, "importe") || strings.Contains(lower, "quantity"):
		return reAmount.FindString(msg)
	case strings.Contains(lower, "service"):
		// "the billing service" / "el servicio billing"
		if m := reServiceEN.FindStringSubmatch(msg); m != nil && !stopwords[strings.ToLower(m[1])] {
			return m[1]
		}
		if m := reServiceES.FindStringSubmatch(msg); m != nil {
			return m[1]
		}
	case strings.HasSuffix(lower, "id") || strings.Contains(lower, "number") || strings.Contains(lower, "code"):
		for _, candidate := range reIDLike.FindAllString(msg, -1) {
			if !reDate.MatchString(candidate) {
				return candidate
			}
		}
	case lower == "request" || lower == "description" || lower == "message" || lower == "query" || lower == "fact":
		if m := reQuoted.FindStringSubmatch(msg); m != nil {
			for _, g := range m[1:] {
				if g != "" {
					return g
				}
			}
		}
		return strings.TrimSpace(msg)
	}
	if m := reQuoted.FindStringSubmatch(msg); m != nil {
		for _, g := range m[1:] {
			if g != "" {
				return g
			}
		}
	}
	return ""
}

var spanishHints = map[string]bool{
	"el": true, "la": true, "de": true, "que": true, "por": true, "para": true, "mi": true,
	"consulta": true, "dame": true, "quiero": true, "cliente": true, "saldo": true, "tarjeta": true,
	"preselecciona": true, "traduce": true, "puntua": true, "caso": true, "credito": true,
}

func detectLanguage(msg string) string {
	if strings.TrimSpace(msg) == "" {
		return "und"
	}
	if strings.ContainsAny(msg, "ñáéíóúü¿¡") {
		return "es"
	}
	for _, w := range strings.Fields(stripAccents(strings.ToLower(msg))) {
		if spanishHints[strings.Trim(w, ".,;:!?")] {
			return "es"
		}
	}
	return "en"
}
