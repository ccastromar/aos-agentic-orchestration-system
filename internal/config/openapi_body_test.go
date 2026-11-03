package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestOpenAPIRequestBodyTemplate(t *testing.T) {
	var spec openAPISpec
	err := json.Unmarshal([]byte(`{
	  "paths": {
	    "/inline": {"post": {"requestBody": {"content": {"application/json": {"schema": {"properties": {"a": {}, "b": {}}}}}}}},
	    "/ref":    {"post": {"requestBody": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Thing"}}}}}},
	    "/form":   {"post": {"requestBody": {"content": {"application/x-www-form-urlencoded": {"schema": {"properties": {"x": {}}}}}}}},
	    "/none":   {"get": {}}
	  },
	  "components": {"schemas": {"Thing": {"properties": {"name": {}}}}}
	}`), &spec)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]map[string]string{
		"/inline": {"a": "{{ .a }}", "b": "{{ .b }}"},
		"/ref":    {"name": "{{ .name }}"},
		"/form":   nil, // only JSON bodies are built
		"/none":   nil,
	}
	for path, want := range cases {
		for _, op := range spec.Paths[path] {
			if got := requestBodyTemplate(spec, op); !reflect.DeepEqual(got, want) {
				t.Errorf("%s: body = %v, want %v", path, got, want)
			}
		}
	}
}
