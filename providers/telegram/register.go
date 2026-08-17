package telegram

import (
	"fmt"

	notify "github.com/Wikid82/go_notify_yourself"
	"github.com/Wikid82/go_notify_yourself/providers/internal/regconfig"
	"github.com/Wikid82/go_notify_yourself/transport"
)

// init registers this package's Factory under the name "telegram" with the
// notify package's registry.
//
// Expected config keys:
//   - "transport" (required): *transport.Wrapper — the shared dispatch
//     primitive; see transport.NewWrapper.
//   - "bot_token" (string): the Telegram bot token.
//   - "chat_id" (string): the destination chat ID.
//   - "base_url" (string, optional): overrides the Telegram Bot API base;
//     intended for tests.
//   - "template" (string, optional): "minimal" (default), "detailed", or
//     "custom".
//   - "custom_template" (string, optional): used when template is "custom".
func init() {
	notify.Register("telegram", func(config map[string]any) (notify.Sender, error) {
		w, ok := config["transport"].(*transport.Wrapper)
		if !ok || w == nil {
			return nil, fmt.Errorf(`telegram: config["transport"] must be a non-nil *transport.Wrapper`)
		}
		cfg := Config{
			BotToken:       regconfig.StringField(config, "bot_token"),
			ChatID:         regconfig.StringField(config, "chat_id"),
			BaseURL:        regconfig.StringField(config, "base_url"),
			Template:       regconfig.StringField(config, "template"),
			CustomTemplate: regconfig.StringField(config, "custom_template"),
		}
		return New(cfg, w), nil
	})
}
