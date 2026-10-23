package auth

import (
	"fmt"
	"strings"
)

// DefaultRoleClaim is the claim AOS reads roles from when none is configured.
const DefaultRoleClaim = "roles"

// ParseRoleMap parses "external=internal,external2=internal2". An empty string means no mapping:
// token roles are used as they are.
func ParseRoleMap(s string) (map[string]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	m := map[string]string{}
	for _, entry := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(entry), "=")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if !ok || k == "" || v == "" {
			return nil, fmt.Errorf("invalid role mapping entry %q", entry)
		}
		if _, dup := m[k]; dup {
			return nil, fmt.Errorf("duplicate role mapping for %q", k)
		}
		m[k] = v
	}
	return m, nil
}

// extractRoles reads the role list at claim (a dotted path such as realm_access.roles, so any
// identity provider's layout works), accepting an array or a space/comma separated string, and
// translates it with roleMap. When a map is configured, unmapped roles grant nothing.
func extractRoles(claims map[string]interface{}, claim string, roleMap map[string]string) []string {
	if claim == "" {
		claim = DefaultRoleClaim
	}
	var cur interface{} = claims
	for _, part := range strings.Split(claim, ".") {
		obj, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		if cur, ok = obj[part]; !ok {
			return nil
		}
	}
	var raw []string
	switch v := cur.(type) {
	case []interface{}:
		for _, x := range v {
			if s, ok := x.(string); ok {
				raw = append(raw, s)
			}
		}
	case string:
		raw = strings.FieldsFunc(v, func(r rune) bool { return r == ' ' || r == ',' })
	}
	var roles []string
	seen := map[string]bool{}
	for _, r := range raw {
		if r = strings.TrimSpace(r); r == "" {
			continue
		}
		if roleMap != nil {
			mapped, ok := roleMap[r]
			if !ok {
				continue
			}
			r = mapped
		}
		if !seen[r] {
			seen[r] = true
			roles = append(roles, r)
		}
	}
	return roles
}
