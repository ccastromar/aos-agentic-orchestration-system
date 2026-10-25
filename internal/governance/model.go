package governance

// Governance functions (Chestertown contract v1, contract/README.md): which functions each IdP
// group or role grants, validated against the contract's rules and floors. The contract
// directory is a copy synced from prudence-governance; do not edit it here.

import (
	_ "embed"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const ModelVersion = "chestertown.governance/v1"

//go:embed contract/default-model.yaml
var defaultModel []byte

type functionKind int

const (
	personFunction functionKind = iota
	machineFunction
)

type functionSpec struct {
	kind functionKind
	apps map[string]bool
}

func apps(names ...string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

// contract/README.md, section 2.
var functions = map[string]functionSpec{
	"system.declare":  {personFunction, apps("prudence")},
	"system.approve":  {personFunction, apps("prudence")},
	"policy.author":   {personFunction, apps("prudence")},
	"policy.approve":  {personFunction, apps("prudence")},
	"oversight":       {personFunction, apps("aos", "prudence")},
	"incident.handle": {personFunction, apps("prudence")},
	"ai.use":          {personFunction, apps("aos")},
	"evidence.write":  {personFunction, apps("ernest")},
	"audit":           {personFunction, apps("aos", "prudence", "ernest")},
	"platform.admin":  {personFunction, apps("aos", "prudence", "ernest")},
	"runtime.decide":  {machineFunction, apps("prudence")},
	"runtime.report":  {machineFunction, apps("prudence")},
}

var (
	knownApps        = apps("aos", "prudence", "ernest")
	tiers            = map[string]bool{"HIGH": true, "LIMITED": true, "MINIMAL": true}
	accountabilities = map[string]bool{"OWNER": true, "SUPERVISOR": true, "COMPLIANCE": true, "DPO": true, "SECURITY": true}
	grantPattern     = regexp.MustCompile(`^([a-z]+(?:\.[a-z]+)?)(?::([a-z0-9][a-z0-9_-]*|\*))?(?:@([a-z]+))?$`)
)

// ModelError is an invalid model; Code is the contract's error code.
type ModelError struct {
	Code   string
	Detail string
}

func (e *ModelError) Error() string { return e.Code + ": " + e.Detail }

func modelErr(code, format string, args ...any) error {
	return &ModelError{Code: code, Detail: fmt.Sprintf(format, args...)}
}

type grant struct {
	function, domain, app string
}

// Model is a validated governance model.
type Model struct {
	peopleClaim, machinesClaim string
	grants                     map[string][]grant
}

// LoadModel reads the model at path, or the contract's default model when path is empty.
func LoadModel(path string) (*Model, error) {
	data := defaultModel
	if path != "" {
		var err error
		if data, err = os.ReadFile(path); err != nil {
			return nil, err
		}
	}
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, modelErr("model.version", "unreadable model: %v", err)
	}
	return NewModel(raw)
}

// NewModel validates a parsed model (contract/README.md, section 4) and returns the first rule
// it breaks as a *ModelError.
func NewModel(raw any) (*Model, error) {
	root, ok := raw.(map[string]any)
	if !ok || root["version"] != ModelVersion {
		return nil, modelErr("model.version", "expected %s", ModelVersion)
	}
	people, err := claimOf(root["identity"], "people")
	if err != nil {
		return nil, err
	}
	machines, err := claimOf(root["identity"], "machines")
	if err != nil {
		return nil, err
	}

	rawMap, ok := root["map"].(map[string]any)
	if !ok {
		return nil, modelErr("model.map", "map must be an object")
	}
	m := &Model{peopleClaim: people, machinesClaim: machines, grants: map[string][]grant{}}
	for key, value := range rawMap {
		list, ok := value.([]any)
		if key == "" || !ok || len(list) == 0 {
			return nil, modelErr("model.map", "entry %q", key)
		}
		kinds := map[functionKind]bool{}
		for _, g := range list {
			parsed, err := parseGrant(g)
			if err != nil {
				return nil, err
			}
			kinds[functions[parsed.function].kind] = true
			m.grants[key] = append(m.grants[key], parsed)
		}
		if len(kinds) > 1 {
			return nil, modelErr("grant.mixed_kinds", "%s", key)
		}
	}

	rules, _ := root["rules"].(map[string]any)
	if err := validateApprovals(rules["approvals"]); err != nil {
		return nil, err
	}
	if err := validateIncompatible(rules["incompatible"]); err != nil {
		return nil, err
	}
	if err := validateAccountable(rules["accountable"]); err != nil {
		return nil, err
	}
	return m, nil
}

// Resolve returns the functions app grants to a token with these claims, sorted
// (contract/README.md, section 5).
func (m *Model) Resolve(claims map[string]any, app string) []string {
	granted := map[string]bool{}
	m.collect(granted, claimValues(claims, m.peopleClaim), personFunction, app)
	m.collect(granted, claimValues(claims, m.machinesClaim), machineFunction, app)
	out := make([]string, 0, len(granted))
	for f := range granted {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

func (m *Model) collect(granted map[string]bool, values []string, kind functionKind, app string) {
	for _, value := range values {
		for _, g := range m.grants[value] {
			spec := functions[g.function]
			if spec.kind != kind || !spec.apps[app] || (g.app != "" && g.app != app) {
				continue
			}
			if g.domain != "" {
				granted[g.function+":"+g.domain] = true
			} else {
				granted[g.function] = true
			}
		}
	}
}

func claimValues(claims map[string]any, name string) []string {
	var value any = claims
	for _, part := range strings.Split(name, ".") {
		obj, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		if value, ok = obj[part]; !ok {
			return nil
		}
	}
	switch v := value.(type) {
	case string:
		return strings.Fields(v)
	case []any:
		var out []string
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	}
	return nil
}

func parseGrant(raw any) (grant, error) {
	s, ok := raw.(string)
	if !ok {
		return grant{}, modelErr("grant.syntax", "%v", raw)
	}
	m := grantPattern.FindStringSubmatch(s)
	if m == nil {
		return grant{}, modelErr("grant.syntax", "%s", s)
	}
	g := grant{function: m[1], domain: m[2], app: m[3]}
	spec, known := functions[g.function]
	switch {
	case !known:
		return grant{}, modelErr("grant.unknown_function", "%s", s)
	case g.domain != "" && g.function != "ai.use":
		return grant{}, modelErr("grant.domain_not_allowed", "%s", s)
	case g.domain == "" && g.function == "ai.use":
		return grant{}, modelErr("grant.domain_required", "%s", s)
	case g.app != "" && !knownApps[g.app]:
		return grant{}, modelErr("grant.unknown_app", "%s", s)
	case g.app != "" && !spec.apps[g.app]:
		return grant{}, modelErr("grant.app_not_applicable", "%s", s)
	}
	return g, nil
}

func claimOf(identity any, kind string) (string, error) {
	if id, ok := identity.(map[string]any); ok {
		if section, ok := id[kind].(map[string]any); ok {
			if claim, ok := section["claim"].(string); ok && claim != "" {
				return claim, nil
			}
		}
	}
	return "", modelErr("model.identity", "identity.%s.claim", kind)
}

// count returns an integer approvals count; booleans and other types are not counts.
func count(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		if n == float64(int(n)) {
			return int(n), true
		}
	}
	return 0, false
}

func validateApprovals(raw any) error {
	approvals := map[string]any{}
	if raw != nil {
		var ok bool
		if approvals, ok = raw.(map[string]any); !ok {
			return modelErr("rules.approvals", "approvals must be an object")
		}
	}
	for tier, rule := range approvals {
		r, ok := rule.(map[string]any)
		n, isCount := count(r["count"])
		if !tiers[tier] || !ok || !isCount || n < 1 {
			return modelErr("rules.approvals", "%s", tier)
		}
	}
	high, _ := approvals["HIGH"].(map[string]any)
	n, _ := count(high["count"])
	if high == nil || n < 2 || high["distinct"] != true {
		return modelErr("floor.approvals.HIGH", "HIGH needs count >= 2 and distinct: true")
	}
	return nil
}

func validateIncompatible(raw any) error {
	if raw == nil {
		return nil
	}
	pairs, ok := raw.([]any)
	if !ok {
		return modelErr("rules.incompatible", "incompatible must be a list")
	}
	for _, p := range pairs {
		pair, ok := p.([]any)
		// Both must be function names before comparing: == on two lists would panic.
		if !ok || len(pair) != 2 || !isPersonFunction(pair[0]) || !isPersonFunction(pair[1]) || pair[0] == pair[1] {
			return modelErr("rules.incompatible", "%v", p)
		}
	}
	return nil
}

func isPersonFunction(v any) bool {
	name, ok := v.(string)
	spec, known := functions[name]
	return ok && known && spec.kind == personFunction
}

func validateAccountable(raw any) error {
	accountable := map[string]any{}
	if raw != nil {
		var ok bool
		if accountable, ok = raw.(map[string]any); !ok {
			return modelErr("rules.accountable", "accountable must be an object")
		}
	}
	for tier, roles := range accountable {
		list, ok := roles.([]any)
		if !tiers[tier] || !ok {
			return modelErr("rules.accountable", "%s", tier)
		}
		for _, r := range list {
			if s, ok := r.(string); !ok || !accountabilities[s] {
				return modelErr("rules.accountable", "%s", tier)
			}
		}
	}
	high, _ := accountable["HIGH"].([]any)
	for _, r := range high {
		if r == "SUPERVISOR" {
			return nil
		}
	}
	return modelErr("floor.accountable.HIGH", "HIGH needs SUPERVISOR")
}
