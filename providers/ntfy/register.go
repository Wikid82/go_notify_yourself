package ntfy

import (
	"fmt"

	notify "github.com/Wikid82/go_notify_yourself"
	"github.com/Wikid82/go_notify_yourself/providers/internal/regconfig"
	"github.com/Wikid82/go_notify_yourself/transport"
)

// init registers this package's Factory under the name "ntfy" with the
// notify package's registry.
//
// Expected config keys:
//   - "transport" (required): *transport.Wrapper — the shared dispatch
//     primitive; see transport.NewWrapper.
//   - "url" (string): the ntfy topic push endpoint.
//   - "token" (string, optional): an ntfy access token.
//   - "template" (string, optional): "minimal" (default), "detailed", or
//     "custom".
//   - "custom_template" (string, optional): used when template is "custom".
func init() {
	notify.Register("ntfy", func(config map[string]any) (notify.Sender, error) {
		w, ok := config["transport"].(*transport.Wrapper)
		if !ok || w == nil {
			return nil, fmt.Errorf(`ntfy: config["transport"] must be a non-nil *transport.Wrapper`)
		}
		cfg := Config{
			URL:            regconfig.StringField(config, "url"),
			Token:          regconfig.StringField(config, "token"),
			Template:       regconfig.StringField(config, "template"),
			CustomTemplate: regconfig.StringField(config, "custom_template"),
		}
		return New(cfg, w), nil
	})
}
