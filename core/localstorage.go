package core

import (
	"encoding/json"
	"strings"
	"unicode/utf16"
)

// decodeLocalStorageValue decodes a Chromium Local Storage LevelDB value. The
// first byte is an encoding tag: 0 = UTF-16LE, 1 = Latin-1/UTF-8. Any other
// leading byte is returned as-is.
func decodeLocalStorageValue(v []byte) string {
	if len(v) == 0 {
		return ""
	}
	switch v[0] {
	case 0:
		u := make([]uint16, 0, len(v)/2)
		for i := 1; i+1 < len(v); i += 2 {
			u = append(u, uint16(v[i])|uint16(v[i+1])<<8)
		}
		return string(utf16.Decode(u))
	case 1:
		return string(v[1:])
	default:
		return string(v)
	}
}

// parseJSONLoose attempts to parse decoded as JSON, with a fallback to find and
// parse the first '{' or '[' substring. Returns the parsed root and ok=true on
// success, or (nil, false) when both attempts fail.
func parseJSONLoose(decoded string) (interface{}, bool) {
	var root interface{}
	if json.Unmarshal([]byte(decoded), &root) == nil {
		return root, true
	}
	i := strings.IndexAny(decoded, "{[")
	if i < 0 {
		return nil, false
	}
	if json.Unmarshal([]byte(decoded[i:]), &root) == nil {
		return root, true
	}
	return nil, false
}

// walkJSON recursively traverses the JSON value tree, calling visit on every
// map[string]interface{}. It handles nested objects and arrays gracefully.
func walkJSON(v interface{}, visit func(map[string]interface{})) {
	switch t := v.(type) {
	case map[string]interface{}:
		visit(t)
		for _, vv := range t {
			walkJSON(vv, visit)
		}
	case []interface{}:
		for _, vv := range t {
			walkJSON(vv, visit)
		}
	}
}

// extractOrgs pulls every organization ({name, rate_limit_tier, billing_type})
// out of a decoded Local Storage value. It walks any nested JSON, collecting each
// object that has a "rate_limit_tier" field. Returns nil for non-JSON or org-free
// input (never panics).
func extractOrgs(decoded string) []orgInfo {
	root, ok := parseJSONLoose(decoded)
	if !ok {
		return nil
	}
	var out []orgInfo
	walkJSON(root, func(t map[string]interface{}) {
		if tier, ok := t["rate_limit_tier"].(string); ok {
			name, _ := t["name"].(string)
			billing, _ := t["billing_type"].(string)
			out = append(out, orgInfo{Name: name, Tier: tier, Billing: billing})
		}
	})
	return out
}
