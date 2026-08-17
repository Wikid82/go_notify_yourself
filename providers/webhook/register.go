package webhook

import (
	"fmt"

	notify "github.com/Wikid82/go_notify_yourself"
	"github.com/Wikid82/go_notify_yourself/providers/internal/regconfig"
	"github.com/Wikid82/go_notify_yourself/transport"
)

// init registers this package's Factory under the name "webhook" with the
// notify package's registry.
//
// Expected config keys:
//   - "transport" (required): *transport.Wrapper — the shared dispatch
//     primitive; see transport.NewWrapper.
//   - "url" (string): the arbitrary destination to POST to.
//   - "template" (string, optional): "minimal" (default), "detailed", or
//     "custom".
//   - "custom_template" (string, optional): used when template is "custom".
func init() {
	notify.Register("webhook", func(config map[string]any) (notify.Sender, error) {
		w, ok := config["transport"].(*transport.Wrapper)
		if !ok || w == nil {
			return nil, fmt.Errorf(`webhook: config["transport"] must be a non-nil *transport.Wrapper`)
		}
		cfg := Config{
			URL:            regconfig.StringField(config, "url"),
			Template:       regconfig.StringField(config, "template"),
			CustomTemplate: regconfig.StringField(config, "custom_template"),
		}
		return New(cfg, w), nil
	})
}
