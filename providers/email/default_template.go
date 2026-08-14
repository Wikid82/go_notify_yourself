package email

import (
	"bytes"
	"fmt"
	"html/template"
	"time"

	notify "github.com/Wikid82/go_notify_yourself"
)

// defaultHTML is the module's single built-in email template: neutral,
// unbranded, inline-styled (no external stylesheet, no logo, no product
// name) so a fresh adopter gets a working, reasonably presentable email
// with zero configuration. Uses html/template, which auto-escapes every
// interpolated field for the HTML context.
var defaultHTML = template.Must(template.New("default").Parse(`<!DOCTYPE html>
<html>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif; background:#f5f5f5; margin:0; padding:24px;">
  <div style="max-width:480px; margin:0 auto; background:#ffffff; border-radius:8px; padding:24px; border:1px solid #e0e0e0;">
    {{if .Title}}<h2 style="margin:0 0 12px; font-size:18px; color:#1a1a1a;">{{.Title}}</h2>{{end}}
    {{if .Body}}<p style="margin:0 0 16px; font-size:14px; line-height:1.5; color:#333333; white-space:pre-wrap;">{{.Body}}</p>{{end}}
    <p style="margin:16px 0 0; font-size:12px; color:#999999;">{{if .EventType}}{{.EventType}} &middot; {{end}}{{.Timestamp}}</p>
  </div>
</body>
</html>
`))

type defaultTemplateData struct {
	Title     string
	Body      string
	EventType string
	Timestamp string
}

// defaultTemplateRenderer implements TemplateRenderer using defaultHTML.
// It ignores the templateName argument entirely — there is exactly one
// built-in template — per the extraction spec's decided tradeoff (§3.3.4):
// ship one neutral default, hosts override Renderer for anything branded
// or template-name-differentiated.
type defaultTemplateRenderer struct{}

func (defaultTemplateRenderer) Render(_ string, msg notify.Message) (string, error) {
	msg = msg.Normalized()
	var buf bytes.Buffer
	data := defaultTemplateData{
		Title:     msg.Title,
		Body:      msg.Body,
		EventType: msg.EventType,
		Timestamp: msg.Timestamp.Format(time.RFC3339),
	}
	if err := defaultHTML.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render default email template: %w", err)
	}
	return buf.String(), nil
}
