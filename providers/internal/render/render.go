// Package render is the shared Go text/template execution engine used by
// every providers/* package to turn a provider's configured template
// string (built-in minimal/detailed, or a user-supplied custom template)
// into a JSON payload. It is unexported (internal) because it is
// implementation plumbing shared across sibling provider packages, not
// part of this module's public API.
package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
	"time"

	notify "github.com/Wikid82/go_notify_yourself"
)

// MaxTemplateSize is the maximum accepted length, in bytes, of a template
// string before parsing is attempted.
const MaxTemplateSize = 10 * 1024

// ExecTimeout bounds how long template execution is allowed to run before
// Render gives up and returns an error. Guards against pathological
// templates (e.g. deeply nested range/recursion) hanging a dispatch.
const ExecTimeout = 5 * time.Second

// MinimalTemplate is the built-in "minimal" JSON template shared by every
// provider package that supports JSON templates. It mirrors Charon's
// original minimal template field names exactly.
const MinimalTemplate = `{"message": {{toJSON .Message}}, "title": {{toJSON .Title}}, "time": {{toJSON .Time}}, "event": {{toJSON .EventType}}}`

// DetailedTemplate is the built-in "detailed" JSON template. Unlike
// Charon's original (which exposed HostName/HostIP/ServiceCount/Services
// as top-level JSON fields), host-specific extras are nested under "data"
// via notify.Message.Data — see TemplateData for the full field mapping.
// This is a deliberate, documented payload-shape change from the ported
// original (spec §3.6 step 5 / §7 risk 1b).
const DetailedTemplate = `{"title": {{toJSON .Title}}, "message": {{toJSON .Message}}, "time": {{toJSON .Time}}, "event": {{toJSON .EventType}}, "data": {{toJSON .Data}}}`

// funcMap supplies the "toJSON" template helper used by every built-in and
// custom template.
var funcMap = template.FuncMap{
	"toJSON": func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	},
}

// SelectTemplate resolves which template string to use given a selector
// ("minimal" | "detailed" | "custom" | "" ) and a possibly-empty custom
// template string, falling back to minimalTemplate when selector is
// unrecognized/empty and custom is also empty. Mirrors Charon's
// sendJSONPayload/RenderTemplate template-selection switch.
func SelectTemplate(selector, custom, minimalTemplate, detailedTemplate string) string {
	switch strings.ToLower(strings.TrimSpace(selector)) {
	case "detailed":
		return detailedTemplate
	case "minimal":
		return minimalTemplate
	default: // "custom" and any unrecognized/empty selector
		if custom == "" {
			return minimalTemplate
		}
		return custom
	}
}

// TemplateData adapts a notify.Message into the field names Charon's
// original built-in templates (and any pre-existing custom templates)
// expect: Title, Message (from msg.Body), Time (RFC3339-formatted string —
// matching the original's data["Time"] string format, not Go's default
// nanosecond-precision JSON time encoding), EventType, and Data
// (host-supplied extras, replacing the old flat
// HostName/HostIP/ServiceCount/Services fields — see DetailedTemplate).
func TemplateData(msg notify.Message) map[string]any {
	msg = msg.Normalized()
	return map[string]any{
		"Title":     msg.Title,
		"Message":   msg.Body,
		"Time":      msg.Timestamp.Format(time.RFC3339),
		"EventType": msg.EventType,
		"Data":      msg.Data,
	}
}

// Render parses tmplStr as a Go text/template (with the "toJSON" helper
// available) and executes it against data, returning the rendered output
// as a string. Enforces MaxTemplateSize on the template source and
// ExecTimeout on execution.
func Render(tmplStr string, data any) (string, error) {
	if len(tmplStr) > MaxTemplateSize {
		return "", fmt.Errorf("template size exceeds maximum limit of %d bytes", MaxTemplateSize)
	}

	tmpl, err := template.New("notify").Funcs(funcMap).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var body bytes.Buffer
	execDone := make(chan error, 1)
	go func() {
		execDone <- tmpl.Execute(&body, data)
	}()

	select {
	case execErr := <-execDone:
		if execErr != nil {
			return "", fmt.Errorf("failed to execute template: %w", execErr)
		}
	case <-time.After(ExecTimeout):
		return "", fmt.Errorf("template execution timed out after %s", ExecTimeout)
	}

	return body.String(), nil
}
