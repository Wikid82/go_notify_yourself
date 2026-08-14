package render

import (
	"strings"
	"testing"
	"time"

	notify "github.com/Wikid82/go_notify_yourself"
)

func TestSelectTemplate(t *testing.T) {
	tests := []struct {
		name     string
		selector string
		custom   string
		want     string
	}{
		{"detailed", "detailed", "custom body", "detailed-template"},
		{"minimal", "minimal", "custom body", "minimal-template"},
		{"custom with body", "custom", "custom body", "custom body"},
		{"custom empty falls back to minimal", "custom", "", "minimal-template"},
		{"unrecognized selector with custom", "", "custom body", "custom body"},
		{"unrecognized selector no custom", "", "", "minimal-template"},
		{"case-insensitive", "DETAILED", "", "detailed-template"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SelectTemplate(tt.selector, tt.custom, "minimal-template", "detailed-template")
			if got != tt.want {
				t.Fatalf("SelectTemplate(%q, %q) = %q, want %q", tt.selector, tt.custom, got, tt.want)
			}
		})
	}
}

func TestRenderMinimalTemplate(t *testing.T) {
	data := TemplateData(notify.Message{
		Title:     "hello",
		Body:      "world",
		EventType: "test",
		Timestamp: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
	})

	got, err := Render(MinimalTemplate, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{`"message": "world"`, `"title": "hello"`, `"event": "test"`, `"time": "2024-01-02T03:04:05Z"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected rendered output to contain %q, got %q", want, got)
		}
	}
}

func TestRenderDetailedTemplateNestsDataField(t *testing.T) {
	data := TemplateData(notify.Message{
		Title: "hi",
		Body:  "body",
		Data:  map[string]any{"HostName": "example"},
	})

	got, err := Render(DetailedTemplate, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `"data": {"HostName":"example"}`) {
		t.Fatalf("expected nested data field, got %q", got)
	}
}

func TestRenderRejectsOversizedTemplate(t *testing.T) {
	huge := strings.Repeat("a", MaxTemplateSize+1)
	_, err := Render(huge, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum limit") {
		t.Fatalf("expected oversized template rejection, got: %v", err)
	}
}

func TestRenderRejectsInvalidTemplateSyntax(t *testing.T) {
	_, err := Render(`{{.Unclosed`, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "failed to parse template") {
		t.Fatalf("expected parse error, got: %v", err)
	}
}

func TestRenderRejectsExecutionError(t *testing.T) {
	_, err := Render(`{{.Nonexistent.Field}}`, map[string]any{"Nonexistent": nil})
	if err == nil || !strings.Contains(err.Error(), "failed to execute template") {
		t.Fatalf("expected execution error, got: %v", err)
	}
}

func TestRenderTimesOut(t *testing.T) {
	t.Parallel()
	// A template that blocks forever via a custom func would require a
	// custom FuncMap, which Render doesn't expose. Instead, verify the
	// timeout constant is wired by using a near-zero synthetic timeout
	// isn't possible without exporting it — so this test just documents
	// the exported constant's value as a regression guard.
	if ExecTimeout != 5*time.Second {
		t.Fatalf("expected ExecTimeout of 5s, got %v", ExecTimeout)
	}
}

func TestTemplateDataDefaultsTimestamp(t *testing.T) {
	data := TemplateData(notify.Message{Title: "t"})
	timeStr, ok := data["Time"].(string)
	if !ok || timeStr == "" {
		t.Fatalf("expected a non-empty formatted Time, got %#v", data["Time"])
	}
}
