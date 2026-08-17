package gotify

import (
	"testing"

	notify "github.com/Wikid82/go_notify_yourself"
	"github.com/Wikid82/go_notify_yourself/transport"
)

func TestRegister_NewReturnsWorkingSender(t *testing.T) {
	w := transport.NewWrapper()

	sender, err := notify.New("gotify", map[string]any{
		"transport": w,
		"url":       "https://gotify.example.com/message",
		"token":     "abc123",
	})
	if err != nil {
		t.Fatalf("notify.New(\"gotify\", ...) returned error: %v", err)
	}

	client, ok := sender.(*Client)
	if !ok {
		t.Fatalf("expected *gotify.Client, got %T", sender)
	}
	if client.cfg.URL != "https://gotify.example.com/message" || client.cfg.Token != "abc123" {
		t.Errorf("expected config to be threaded through, got %#v", client.cfg)
	}
}

func TestRegister_MissingTransportReturnsErrorNotPanic(t *testing.T) {
	sender, err := notify.New("gotify", map[string]any{
		"url": "https://gotify.example.com/message",
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
		if name == "gotify" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected %q registered in notify.RegisteredTypes(), got %v", "gotify", notify.RegisteredTypes())
	}
}
