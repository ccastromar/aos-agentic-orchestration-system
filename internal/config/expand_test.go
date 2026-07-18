package config

import "testing"

func TestExpandEnvRefs(t *testing.T) {
	t.Setenv("AOS_TEST_BASE", "http://mocks:8080")

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"set var", "${AOS_TEST_BASE}/mock/x", "http://mocks:8080/mock/x"},
		{"set var ignores default", "${AOS_TEST_BASE:-http://localhost:9000}/mock/x", "http://mocks:8080/mock/x"},
		{"unset var uses default", "${AOS_TEST_UNSET:-http://localhost:9000}/mock/x", "http://localhost:9000/mock/x"},
		{"unset var no default", "${AOS_TEST_UNSET}/mock/x", "/mock/x"},
		{"go template untouched", "${AOS_TEST_UNSET:-http://localhost:9000}/mock/x?id={{ .id }}", "http://localhost:9000/mock/x?id={{ .id }}"},
		{"plain string untouched", "http://localhost:9000/mock/x", "http://localhost:9000/mock/x"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := expandEnvRefs(tc.in); got != tc.want {
				t.Errorf("expandEnvRefs(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
