package auth

import "strings"

// LegacyFunctions gives identities that do not come from the identity provider (the development
// login and mock, IAM API keys) the governance functions their AOS roles always implied: each
// role is an ai.use domain, and admin also operates the platform. Tokens from the IdP get their
// functions from the governance model instead (JWTConfig.Governance).
func LegacyFunctions(roles []string) []string {
	var functions []string
	for _, r := range roles {
		if r == "" {
			continue
		}
		functions = append(functions, "ai.use:"+r)
		if r == "admin" {
			functions = append(functions, "platform.admin")
		}
	}
	return functions
}

// HasFunction reports whether the identity holds a governance function.
func (i *Identity) HasFunction(function string) bool {
	if i == nil {
		return false
	}
	for _, f := range i.Functions {
		if f == function {
			return true
		}
	}
	return false
}

// UseDomains returns the ai.use domains the identity holds ("*" for every domain).
func (i *Identity) UseDomains() []string {
	if i == nil {
		return nil
	}
	var domains []string
	for _, f := range i.Functions {
		if d, ok := strings.CutPrefix(f, "ai.use:"); ok {
			domains = append(domains, d)
		}
	}
	return domains
}
