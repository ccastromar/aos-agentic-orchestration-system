package tools

import (
	"bytes"
	"fmt"
	"os"
	"text/template"
)

//
// -------------------------------------------------------------
// TEMPLATE RENDERING UTILITIES
// -------------------------------------------------------------
//

// RenderTemplateString
// Example:
// "http://localhost:9000/svc?id={{ .id }}"
func RenderTemplateString(tpl string, params map[string]string) (string, error) {
	if params == nil {
		return tpl, nil
	}

	t, err := template.New("tpl").
		Option("missingkey=zero").
		Funcs(template.FuncMap{
			"env": func(name string) string { return os.Getenv(name) },
		}).
		Parse(tpl)
	if err != nil {
		return "", fmt.Errorf("error parsing template string: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, params); err != nil {
		return "", fmt.Errorf("error executing template string: %w", err)
	}

	return buf.String(), nil
}

// RenderTemplateMap process a MAP of strings.
// For the body of the tool
// body:
//
//	customerId: "{{ .customerId }}"
//	days:       "{{ .days }}"
//
// Produces a map[string]string
func RenderTemplateMap(body map[string]string, params map[string]string) (map[string]string, error) {
	if body == nil {
		return map[string]string{}, nil
	}

	out := make(map[string]string)

	for k, v := range body {
		t, err := template.New("body").
			Option("missingkey=zero").
			Funcs(template.FuncMap{
				"env": func(name string) string { return os.Getenv(name) },
			}).
			Parse(v)
		if err != nil {
			return nil, fmt.Errorf("error parsing template body field=%s: %w", k, err)
		}

		var buf bytes.Buffer
		if err := t.Execute(&buf, params); err != nil {
			return nil, fmt.Errorf("error executing template body field=%s: %w", k, err)
		}

		out[k] = buf.String()
	}

	return out, nil
}
