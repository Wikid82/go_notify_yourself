package slack

import (
	"testing"

	notify "github.com/Wikid82/go_notify_yourself"
	"github.com/Wikid82/go_notify_yourself/transport"
)

func TestRegister_NewReturnsWorkingSender(t *testing.T) {
	w := transport.NewWrapper()

	sender, err := notify.New("slack", map[string]any{
		"transport":   w,
		"webhook_url": "https://hooks.slack.com/services/T000/B000/xxxxxxxxxxxxxxxxxxxxxxxx",
		"template":    "minimal",
	})
	if err != nil {
		t.Fatalf("notify.New(\"slack\", ...) returned error: %v", err)
	}

	client, ok := sender.(*Client)
	if !ok {
		t.Fatalf("expected *slack.Client, got %T", sender)
	}
	if client.cfg.WebhookURL != "https://hooks.slack.com/services/T000/B000/xxxxxxxxxxxxxxxxxxxxxxxx" {
		t.Errorf("expected WebhookURL to be threaded through from config, got %q", client.cfg.WebhookURL)
	}
}

func TestRegister_MissingTransportReturnsErrorNotPanic(t *testing.T) {
	sender, err := notify.New("slack", map[string]any{
		"webhook_url": "https://hooks.slack.com/services/T000/B000/xxxxxxxxxxxxxxxxxxxxxxxx",
	})
	if err == nil {
		t.Fatal("expected an error when config[\"transport\"] is missing")
	}
	if sender != nil {
		t.Fatalf("expected a nil Sender on error, got %#v", sender)
	}
}

func TestRegister_WrongTypedTransportReturnsError(t *testing.T) {
	_, err := notify.New("slack", map[string]any{
		"transport":   "not-a-wrapper",
		"webhook_url": "https://hooks.slack.com/services/T000/B000/xxxxxxxxxxxxxxxxxxxxxxxx",
	})
	if err == nil {
		t.Fatal("expected an error when config[\"transport\"] is the wrong type")
	}
}

func TestRegister_RegisteredUnderExpectedName(t *testing.T) {
	found := false
	for _, name := range notify.RegisteredTypes() {
		if name == "slack" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected %q registered in notify.RegisteredTypes(), got %v", "slack", notify.RegisteredTypes())
	}
}
