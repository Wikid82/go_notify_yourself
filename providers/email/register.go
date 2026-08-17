package email

import (
	"fmt"

	notify "github.com/Wikid82/go_notify_yourself"
	"github.com/Wikid82/go_notify_yourself/providers/internal/regconfig"
)

// init registers this package's Factory under the name "email" with the
// notify package's registry.
//
// Unlike the seven HTTP-based providers, email's Config carries
// non-JSON-serializable Go values (Mailer/TemplateRenderer are behavioral
// interfaces; TemplateName is a closure) — a host application constructs
// these at startup and puts the actual values directly into config under
// the well-known keys below, rather than plain data. There is no
// "transport" key: email never dials HTTP directly (see email.go's
// New(cfg Config) *Client, which takes no *transport.Wrapper).
//
// Expected config keys:
//   - "mailer" (required): Mailer — transports the composed email.
//   - "recipients" (optional): []string — destination addresses.
//   - "subject_prefix" (string, optional): prepended to the message title.
//   - "renderer" (optional): TemplateRenderer — renders the HTML body; nil
//     uses the package's built-in neutral template.
//   - "template_name" (optional): func(notify.Message) string — maps a
//     Message to a host-defined template name.
func init() {
	notify.Register("email", func(config map[string]any) (notify.Sender, error) {
		mailer, ok := config["mailer"].(Mailer)
		if !ok || mailer == nil {
			return nil, fmt.Errorf(`email: config["mailer"] must be a non-nil Mailer`)
		}
		cfg := Config{
			Mailer:        mailer,
			SubjectPrefix: regconfig.StringField(config, "subject_prefix"),
			Recipients:    regconfig.StringSliceField(config, "recipients"),
		}
		if r, ok := config["renderer"].(TemplateRenderer); ok {
			cfg.Renderer = r
		}
		if tn, ok := config["template_name"].(func(notify.Message) string); ok {
			cfg.TemplateName = tn
		}
		return New(cfg), nil
	})
}
