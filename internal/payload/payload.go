package payload

// GetString safely returns a string from a map[string]any, or ""/false if missing or wrong type.
func GetString(m map[string]any, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// GetMapAny returns a nested map[string]any safely.
func GetMapAny(m map[string]any, key string) (map[string]any, bool) {
	if m == nil {
		return nil, false
	}
	v, ok := m[key]
	if !ok {
		return nil, false
	}
	mp, ok := v.(map[string]any)
	return mp, ok
}

// GetStringMap tries to convert map[string]any → map[string]string
func GetStringMap(m map[string]any, key string) (map[string]string, bool) {
	src, ok := GetMapAny(m, key)
	if !ok {
		return nil, false
	}
	out := make(map[string]string)
	for k, v := range src {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out, true
}
