package webhook

import (
	"strings"
	"testing"

	notify "github.com/Wikid82/go_notify_yourself"
)

func TestRenderPreviewMinimalTemplate(t *testing.T) {
	rendered, parsed, err := RenderPreview(MinimalTemplate, notify.Message{Title: "t", Body: "b", EventType: "e"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(rendered, `"title": "t"`) {
		t.Fatalf("expected rendered output to contain title, got %q", rendered)
	}
	parsedMap, ok := parsed.(map[string]any)
	if !ok {
		t.Fatalf("expected parsed to be a map, got %T", parsed)
	}
	if parsedMap["title"] != "t" {
		t.Fatalf("expected parsed title 't', got %#v", parsedMap["title"])
	}
}

func TestRenderPreviewDetailedTemplate(t *testing.T) {
	_, parsed, err := RenderPreview(DetailedTemplate, notify.Message{Title: "t", Data: map[string]any{"k": "v"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsedMap := parsed.(map[string]any)
	data, ok := parsedMap["data"].(map[string]any)
	if !ok || data["k"] != "v" {
		t.Fatalf("expected nested data field, got %#v", parsedMap["data"])
	}
}

func TestRenderPreviewInvalidTemplateSyntax(t *testing.T) {
	_, _, err := RenderPreview(`{{.Unclosed`, notify.Message{})
	if err == nil {
		t.Fatal("expected template parse error")
	}
}

func TestRenderPreviewReturnsRenderedOutputEvenOnParseFailure(t *testing.T) {
	rendered, parsed, err := RenderPreview(`not json`, notify.Message{})
	if err == nil {
		t.Fatal("expected JSON parse error")
	}
	if rendered != "not json" {
		t.Fatalf("expected rendered output preserved for caller display, got %q", rendered)
	}
	if parsed != nil {
		t.Fatalf("expected nil parsed value on failure, got %#v", parsed)
	}
}
