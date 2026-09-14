package webpush

import (
	"testing"

	notify "github.com/Wikid82/go_notify_yourself"
	"github.com/Wikid82/go_notify_yourself/transport"
)

func TestRegister_NewReturnsWorkingSender(t *testing.T) {
	w := transport.NewWrapper()

	sender, err := notify.New("webpush", map[string]any{
		"transport":         w,
		"vapid_public_key":  "pub-key",
		"vapid_private_key": "priv-key",
		"vapid_subject":     "mailto:ops@example.com",
		"endpoint":          "https://push.example.net/subscription/abc123",
		"p256dh":            "p256dh-value",
		"auth":              "auth-value",
		"ttl":               3600,
		"urgency":           "high",
		"topic":             "my-topic",
	})
	if err != nil {
		t.Fatalf("notify.New(\"webpush\", ...) returned error: %v", err)
	}

	client, ok := sender.(*Client)
	if !ok {
		t.Fatalf("expected *webpush.Client, got %T", sender)
	}
	want := Config{
		VAPIDPublicKey:  "pub-key",
		VAPIDPrivateKey: "priv-key",
		VAPIDSubject:    "mailto:ops@example.com",
		Endpoint:        "https://push.example.net/subscription/abc123",
		P256dh:          "p256dh-value",
		Auth:            "auth-value",
		TTL:             3600,
		Urgency:         "high",
		Topic:           "my-topic",
	}
	if client.cfg != want {
		t.Errorf("expected config to be threaded through, got %#v, want %#v", client.cfg, want)
	}
}

func TestRegister_TTLAcceptsFloat64FromJSONStyleDecode(t *testing.T) {
	w := transport.NewWrapper()

	sender, err := notify.New("webpush", map[string]any{
		"transport":         w,
		"vapid_public_key":  "pub-key",
		"vapid_private_key": "priv-key",
		"vapid_subject":     "mailto:ops@example.com",
		"endpoint":          "https://push.example.net/subscription/abc123",
		"p256dh":            "p256dh-value",
		"auth":              "auth-value",
		"ttl":               float64(7200),
	})
	if err != nil {
		t.Fatalf("notify.New(\"webpush\", ...) returned error: %v", err)
	}
	client, ok := sender.(*Client)
	if !ok {
		t.Fatalf("expected *webpush.Client, got %T", sender)
	}
	if client.cfg.TTL != 7200 {
		t.Errorf("expected TTL 7200 threaded through from a float64 config value, got %d", client.cfg.TTL)
	}
}

func TestRegister_MissingTransportReturnsErrorNotPanic(t *testing.T) {
	sender, err := notify.New("webpush", map[string]any{
		"vapid_public_key": "pub-key",
	})
	if err == nil {
		t.Fatal("expected an error when config[\"transport\"] is missing")
	}
	if sender != nil {
		t.Fatalf("expected a nil Sender on error, got %#v", sender)
	}
}

func TestRegister_RegisteredUnderExpectedName(t *testing.T) {
	found := false
	for _, name := range notify.RegisteredTypes() {
		if name == "webpush" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected %q registered in notify.RegisteredTypes(), got %v", "webpush", notify.RegisteredTypes())
	}
}
