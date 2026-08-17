package regconfig

import (
	"reflect"
	"testing"
)

func TestStringField(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]any
		key    string
		want   string
	}{
		{"nil config", nil, "webhook_url", ""},
		{"missing key", map[string]any{}, "webhook_url", ""},
		{"present string", map[string]any{"webhook_url": "https://example.com"}, "webhook_url", "https://example.com"},
		{"wrong type int", map[string]any{"webhook_url": 42}, "webhook_url", ""},
		{"wrong type nil value", map[string]any{"webhook_url": nil}, "webhook_url", ""},
		{"empty string value", map[string]any{"webhook_url": ""}, "webhook_url", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StringField(tt.config, tt.key)
			if got != tt.want {
				t.Errorf("StringField(%v, %q) = %q, want %q", tt.config, tt.key, got, tt.want)
			}
		})
	}
}

func TestStringSliceField(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]any
		key    string
		want   []string
	}{
		{"nil config", nil, "recipients", nil},
		{"missing key", map[string]any{}, "recipients", nil},
		{"native string slice", map[string]any{"recipients": []string{"a@example.com", "b@example.com"}}, "recipients", []string{"a@example.com", "b@example.com"}},
		{"any slice of strings", map[string]any{"recipients": []any{"a@example.com", "b@example.com"}}, "recipients", []string{"a@example.com", "b@example.com"}},
		{"empty any slice", map[string]any{"recipients": []any{}}, "recipients", []string{}},
		{"any slice with non-string element", map[string]any{"recipients": []any{"a@example.com", 42}}, "recipients", nil},
		{"wrong type entirely", map[string]any{"recipients": "not-a-slice"}, "recipients", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StringSliceField(tt.config, tt.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("StringSliceField(%v, %q) = %#v, want %#v", tt.config, tt.key, got, tt.want)
			}
		})
	}
}
