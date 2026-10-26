package app

import "testing"

func TestRouteLabelKeepsRoutesNotIds(t *testing.T) {
	for path, want := range map[string]string{
		"/ask":          "/ask",
		"/api/v1/tasks": "/api/v1/tasks",
		"/api/v1/callbacks/s3cr3t-T0ken_abcdefghij":          "/api/v1/callbacks/{token}",
		"/api/v1/tasks/4f1c2a5e-9b0d-4c67-8a21-3e5f6d7c8b90": "/api/v1/tasks/{id}",
		"/api/v1/memories/42":                                "/api/v1/memories/{id}",
		"/health/ready":                                      "/health/ready",
	} {
		if got := routeLabel(path); got != want {
			t.Errorf("routeLabel(%q) = %q, want %q", path, got, want)
		}
	}
}
