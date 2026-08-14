package webhook

import (
	"encoding/json"
	"fmt"

	notify "github.com/Wikid82/go_notify_yourself"
	render "github.com/Wikid82/go_notify_yourself/providers/internal/render"
)

// RenderPreview renders tmplStr against msg without dispatching it —
// intended for a host application's provider-editor UI to validate a
// custom template before saving it. Reusable for previewing any of the
// seven provider types, since they all share the same template mechanism
// (render.Render / render.TemplateData).
//
// Returns the rendered JSON text and its parsed representation. If the
// rendered output isn't valid JSON, rendered is still returned (so the
// caller can show the invalid output to the user) alongside a non-nil
// error.
func RenderPreview(tmplStr string, msg notify.Message) (rendered string, parsed any, err error) {
	rendered, err = render.Render(tmplStr, render.TemplateData(msg))
	if err != nil {
		return "", nil, err
	}

	if err := json.Unmarshal([]byte(rendered), &parsed); err != nil {
		return rendered, nil, fmt.Errorf("failed to parse rendered template: %w", err)
	}

	return rendered, parsed, nil
}
