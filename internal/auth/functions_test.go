package auth

import (
	"reflect"
	"testing"
)

func TestLegacyFunctions(t *testing.T) {
	cases := map[string]struct {
		roles []string
		want  []string
	}{
		"a role is an ai.use domain":   {[]string{"finance"}, []string{"ai.use:finance"}},
		"admin also runs the platform": {[]string{"admin"}, []string{"ai.use:admin", "platform.admin"}},
		// An overseer decides other people's steps and uses no AI domain of their own.
		"oversight is oversight alone": {[]string{"oversight"}, []string{"oversight"}},
		"both, when both are given":    {[]string{"oversight", "finance"}, []string{"oversight", "ai.use:finance"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := LegacyFunctions(tc.roles); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("LegacyFunctions(%v) = %v, want %v", tc.roles, got, tc.want)
			}
		})
	}
}
