package pushover

import (
	"fmt"

	notify "github.com/Wikid82/go_notify_yourself"
	"github.com/Wikid82/go_notify_yourself/providers/internal/regconfig"
	"github.com/Wikid82/go_notify_yourself/transport"
)

// init registers this package's Factory under the name "pushover" with the
// notify package's registry.
//
// Expected config keys:
//   - "transport" (required): *transport.Wrapper — the shared dispatch
//     primitive; see transport.NewWrapper.
//   - "user_key" (string): the Pushover user/group key.
//   - "api_token" (string): the Pushover application API token.
//   - "base_url" (string, optional): overrides Pushover's API base;
//     intended for tests.
//   - "template" (string, optional): "minimal" (default), "detailed", or
//     "custom".
//   - "custom_template" (string, optional): used when template is "custom".
func init() {
	notify.Register("pushover", func(config map[string]any) (notify.Sender, error) {
		w, ok := config["transport"].(*transport.Wrapper)
		if !ok || w == nil {
			return nil, fmt.Errorf(`pushover: config["transport"] must be a non-nil *transport.Wrapper`)
		}
		cfg := Config{
			UserKey:        regconfig.StringField(config, "user_key"),
			APIToken:       regconfig.StringField(config, "api_token"),
			BaseURL:        regconfig.StringField(config, "base_url"),
			Template:       regconfig.StringField(config, "template"),
			CustomTemplate: regconfig.StringField(config, "custom_template"),
		}
		return New(cfg, w), nil
	})
}
