package webpush

import (
	"fmt"

	notify "github.com/Wikid82/go_notify_yourself"
	"github.com/Wikid82/go_notify_yourself/providers/internal/regconfig"
	"github.com/Wikid82/go_notify_yourself/transport"
)

// init registers this package's Factory under the name "webpush" with the
// notify package's registry.
//
// Expected config keys:
//   - "transport" (required): *transport.Wrapper.
//   - "vapid_public_key", "vapid_private_key", "vapid_subject" (string, required).
//   - "endpoint", "p256dh", "auth" (string, required).
//   - "ttl" (int, optional; 0 uses DefaultTTL).
//   - "urgency", "topic" (string, optional).
//   - "template", "custom_template" (string, optional).
func init() {
	notify.Register("webpush", func(config map[string]any) (notify.Sender, error) {
		w, ok := config["transport"].(*transport.Wrapper)
		if !ok || w == nil {
			return nil, fmt.Errorf(`webpush: config["transport"] must be a non-nil *transport.Wrapper`)
		}
		cfg := Config{
			VAPIDPublicKey:  regconfig.StringField(config, "vapid_public_key"),
			VAPIDPrivateKey: regconfig.StringField(config, "vapid_private_key"),
			VAPIDSubject:    regconfig.StringField(config, "vapid_subject"),
			Endpoint:        regconfig.StringField(config, "endpoint"),
			P256dh:          regconfig.StringField(config, "p256dh"),
			Auth:            regconfig.StringField(config, "auth"),
			TTL:             regconfig.IntField(config, "ttl"),
			Urgency:         regconfig.StringField(config, "urgency"),
			Topic:           regconfig.StringField(config, "topic"),
			Template:        regconfig.StringField(config, "template"),
			CustomTemplate:  regconfig.StringField(config, "custom_template"),
		}
		return New(cfg, w), nil
	})
}
