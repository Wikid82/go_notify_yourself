// Package regconfig provides small, shared helpers for decoding a
// notify.Factory's generic map[string]any config into the plain string/
// string-slice fields most provider Config structs need. It is unexported
// (internal) because it is registration-layer plumbing shared across
// sibling providers/* packages, not part of this module's public API.
//
// Every helper here is deliberately lenient: a missing or wrong-typed key
// yields the zero value rather than an error. Each provider's register.go
// is responsible for deciding which of those zero values are actually
// required (e.g. an empty WebhookURL) and returning a descriptive error at
// that point — regconfig only extracts, it never validates provider
// semantics.
package regconfig

// StringField returns config[key] as a string, or "" if the key is absent
// or not a string.
func StringField(config map[string]any, key string) string {
	if config == nil {
		return ""
	}
	v, ok := config[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// IntField returns config[key] as an int, or 0 if the key is absent or not
// an int-like value. Accepts int and int64 (the natural Go-side shapes)
// and float64 (the shape a generic JSON-style decode into map[string]any
// produces, since encoding/json decodes every JSON number as float64) —
// mirroring StringSliceField's existing dual-shape acceptance for []any.
func IntField(config map[string]any, key string) int {
	if config == nil {
		return 0
	}
	v, ok := config[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// StringSliceField returns config[key] as a []string, or nil if the key is
// absent or not a recognized slice-of-string shape. Both []string (the
// natural Go-side shape) and []any of strings (the natural shape after a
// generic decode, e.g. from encoding/json into map[string]any) are
// accepted, so callers that construct config maps either by hand or via
// JSON-style decoding both work without extra conversion.
func StringSliceField(config map[string]any, key string) []string {
	if config == nil {
		return nil
	}
	v, ok := config[key]
	if !ok {
		return nil
	}
	switch vals := v.(type) {
	case []string:
		return vals
	case []any:
		out := make([]string, 0, len(vals))
		for _, item := range vals {
			s, ok := item.(string)
			if !ok {
				return nil
			}
			out = append(out, s)
		}
		return out
	default:
		return nil
	}
}
