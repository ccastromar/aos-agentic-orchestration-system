package engine

import (
	"strconv"
	"strings"
)

func AutoCastVars(vars map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{})
	for k, v := range vars {
		switch val := v.(type) {
		case string:
			// int
			if i, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
				out[k] = i
				continue
			}
			// float
			if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
				out[k] = f
				continue
			}
			// bool
			if b, err := strconv.ParseBool(strings.TrimSpace(val)); err == nil {
				out[k] = b
				continue
			}
			// fallback → string
			out[k] = val

		default:
			// preserve YAML-native types (int, float, bool)
			out[k] = val
		}
	}
	return out
}
