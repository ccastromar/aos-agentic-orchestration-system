package rbac

import "testing"

func TestIsAllowedForFunctions(t *testing.T) {
	c := &Config{Permissions: map[string][]string{
		"banking.get_balance":       {"admin", "finance", "guest"},
		"governance-demo.translate": {"admin", "operator"},
	}}
	c.Defaults.UnknownIntent = "deny"
	cases := []struct {
		name    string
		intent  string
		domains []string
		want    bool
	}{
		{"a listed domain", "banking.get_balance", []string{"finance"}, true},
		{"no listed domain", "governance-demo.translate", []string{"finance", "guest"}, false},
		{"every domain", "governance-demo.translate", []string{"*"}, true},
		{"every domain, unknown intent, deny by default", "crm.delete_everything", []string{"*"}, false},
		{"no domains", "banking.get_balance", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := c.IsAllowedForFunctions(tc.intent, tc.domains); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
