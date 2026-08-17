package slack

import (
	"fmt"

	notify "github.com/Wikid82/go_notify_yourself"
	"github.com/Wikid82/go_notify_yourself/providers/internal/regconfig"
	"github.com/Wikid82/go_notify_yourself/transport"
)

// init registers this package's Factory under the name "slack" with the
// notify package's registry.
//
// Expected config keys:
//   - "transport" (required): *transport.Wrapper — the shared dispatch
//     primitive; see transport.NewWrapper.
//   - "webhook_url" (string): the Slack Incoming Webhook URL.
//   - "template" (string, optional): "minimal" (default), "detailed", or
//     "custom".
//   - "custom_template" (string, optional): used when template is "custom".
func init() {
	notify.Register("slack", func(config map[string]any) (notify.Sender, error) {
		w, ok := config["transport"].(*transport.Wrapper)
		if !ok || w == nil {
			return nil, fmt.Errorf(`slack: config["transport"] must be a non-nil *transport.Wrapper`)
		}
		cfg := Config{
			WebhookURL:     regconfig.StringField(config, "webhook_url"),
			Template:       regconfig.StringField(config, "template"),
			CustomTemplate: regconfig.StringField(config, "custom_template"),
		}
		return New(cfg, w), nil
	})
}
