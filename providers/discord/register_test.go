package discord

import (
	"testing"

	notify "github.com/Wikid82/go_notify_yourself"
	"github.com/Wikid82/go_notify_yourself/transport"
)

func TestRegister_NewReturnsWorkingSender(t *testing.T) {
	w := transport.NewWrapper()

	sender, err := notify.New("discord", map[string]any{
		"transport":   w,
		"webhook_url": "https://discord.com/api/webhooks/123456789/abcDEF",
		"template":    "minimal",
	})
	if err != nil {
		t.Fatalf("notify.New(\"discord\", ...) returned error: %v", err)
	}

	client, ok := sender.(*Client)
	if !ok {
		t.Fatalf("expected *discord.Client, got %T", sender)
	}
	if client.cfg.WebhookURL != "https://discord.com/api/webhooks/123456789/abcDEF" {
		t.Errorf("expected WebhookURL to be threaded through from config, got %q", client.cfg.WebhookURL)
	}
}

func TestRegister_MissingTransportReturnsErrorNotPanic(t *testing.T) {
	sender, err := notify.New("discord", map[string]any{
		"webhook_url": "https://discord.com/api/webhooks/123456789/abcDEF",
	})
	if err == nil {
		t.Fatal("expected an error when config[\"transport\"] is missing")
	}
	if sender != nil {
		t.Fatalf("expected a nil Sender on error, got %#v", sender)
	}
}

func TestRegister_WrongTypedTransportReturnsError(t *testing.T) {
	_, err := notify.New("discord", map[string]any{
		"transport":   "not-a-wrapper",
		"webhook_url": "https://discord.com/api/webhooks/123456789/abcDEF",
	})
	if err == nil {
		t.Fatal("expected an error when config[\"transport\"] is the wrong type")
	}
}

func TestRegister_RegisteredUnderExpectedName(t *testing.T) {
	found := false
	for _, name := range notify.RegisteredTypes() {
		if name == "discord" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected %q registered in notify.RegisteredTypes(), got %v", "discord", notify.RegisteredTypes())
	}
}
