package telegram

import (
	"testing"

	notify "github.com/Wikid82/go_notify_yourself"
	"github.com/Wikid82/go_notify_yourself/transport"
)

func TestRegister_NewReturnsWorkingSender(t *testing.T) {
	w := transport.NewWrapper()

	sender, err := notify.New("telegram", map[string]any{
		"transport": w,
		"bot_token": "bot123:abc",
		"chat_id":   "-100200300",
	})
	if err != nil {
		t.Fatalf("notify.New(\"telegram\", ...) returned error: %v", err)
	}

	client, ok := sender.(*Client)
	if !ok {
		t.Fatalf("expected *telegram.Client, got %T", sender)
	}
	if client.cfg.BotToken != "bot123:abc" || client.cfg.ChatID != "-100200300" {
		t.Errorf("expected config to be threaded through, got %#v", client.cfg)
	}
}

func TestRegister_MissingTransportReturnsErrorNotPanic(t *testing.T) {
	sender, err := notify.New("telegram", map[string]any{
		"bot_token": "bot123:abc",
		"chat_id":   "-100200300",
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
		if name == "telegram" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected %q registered in notify.RegisteredTypes(), got %v", "telegram", notify.RegisteredTypes())
	}
}
