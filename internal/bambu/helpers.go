package bambu

import "strconv"

// Small, defensive accessors for the loosely-typed map[string]any that comes
// out of json.Unmarshal. They mirror the Moonraker normalizer's helpers but
// live here so the bambu package has no dependency on moonraker internals.

func childMap(m map[string]any, k string) map[string]any {
	if m == nil {
		return nil
	}
	if v, ok := m[k].(map[string]any); ok {
		return v
	}
	return nil
}

func getString(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func getFloat(m map[string]any, k string) float64 {
	if m == nil {
		return 0
	}
	switch v := m[k].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		// Bambu sends some numeric-looking fields as strings.
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return 0
}

func numPtr(m map[string]any, k string) (*float64, bool) {
	if m == nil {
		return nil, false
	}
	switch v := m[k].(type) {
	case float64:
		return &v, true
	case int:
		f := float64(v)
		return &f, true
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return &f, true
		}
	}
	return nil, false
}

func intPtr(m map[string]any, k string) (*int, bool) {
	if f, ok := numPtr(m, k); ok {
		i := int(*f)
		return &i, true
	}
	return nil, false
}

func hasAny(m map[string]any, keys ...string) bool {
	if m == nil {
		return false
	}
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func clampFraction(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
