package ollama

import (
	_ "embed"
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

//go:embed scenarios.yaml
var defaultScenarios []byte

// Scenario pins the answer for a demo phrase so the live script never depends on the
// generic keyword matcher. Match is a case-insensitive regular expression over the user
// message; Params values may reference its capture groups ($1, ${name}).
type Scenario struct {
	Name     string            `yaml:"name"`
	Match    string            `yaml:"match"`
	Intent   string            `yaml:"intent"`
	Params   map[string]string `yaml:"params"`
	Defaults map[string]string `yaml:"defaults"` // used when a param expands to ""
	// Summary is the analyst text per language code. It is only used when every step of the
	// run executed without error, so a pinned "success" line can never mask a failure.
	Summary map[string]string `yaml:"summary"`
	// Reply is a canned answer for a free-form prompt that is not one of AOS's fixed prompts
	// (e.g. a framework demo asking for a draft). Match is then applied to the whole prompt.
	Reply string `yaml:"reply"`

	re      *regexp.Regexp
	matched string // the message a returned copy matched, for capture-group expansion
}

type scenarioFile struct {
	Scenarios []Scenario `yaml:"scenarios"`
}

// LoadScenarios reads path, or the embedded default set when path is empty.
func LoadScenarios(path string) ([]Scenario, error) {
	data := defaultScenarios
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read scenarios: %w", err)
		}
		data = b
	}
	var file scenarioFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse scenarios: %w", err)
	}
	for i := range file.Scenarios {
		sc := &file.Scenarios[i]
		if sc.Intent == "" && sc.Reply == "" {
			return nil, fmt.Errorf("scenario %q: intent or reply is required", sc.Name)
		}
		if sc.Reply != "" && sc.Match == "" {
			return nil, fmt.Errorf("scenario %q: a reply needs a match", sc.Name)
		}
		if sc.Match == "" {
			continue // summary-only scenario
		}
		re, err := regexp.Compile("(?i)" + sc.Match)
		if err != nil {
			return nil, fmt.Errorf("scenario %q: invalid match: %w", sc.Name, err)
		}
		sc.re = re
	}
	return file.Scenarios, nil
}

func matchScenario(scenarios []Scenario, msg string) (*Scenario, []int) {
	if msg == "" {
		return nil, nil
	}
	for i := range scenarios {
		sc := &scenarios[i]
		if sc.re == nil || sc.Intent == "" {
			continue
		}
		if groups := sc.re.FindStringSubmatchIndex(msg); groups != nil {
			sc := *sc
			sc.matched = msg
			return &sc, groups
		}
	}
	return nil, nil
}

func (sc *Scenario) render(groups []int) map[string]string {
	out := map[string]string{}
	for k, tmpl := range sc.Params {
		out[k] = string(sc.re.ExpandString(nil, tmpl, sc.matched, groups))
	}
	for k, v := range sc.Defaults {
		if out[k] == "" {
			out[k] = v
		}
	}
	return out
}

func scenarioSummary(scenarios []Scenario, intent, lang string) string {
	for _, sc := range scenarios {
		if sc.Intent == intent && sc.Summary[lang] != "" {
			return sc.Summary[lang]
		}
	}
	return ""
}

// matchReply returns the canned reply for a free-form prompt, if a scenario pins one.
func matchReply(scenarios []Scenario, prompt string) string {
	for _, sc := range scenarios {
		if sc.Reply != "" && sc.re != nil && sc.re.MatchString(prompt) {
			return sc.Reply
		}
	}
	return ""
}
