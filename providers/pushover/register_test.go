package pushover

import (
	"testing"

	notify "github.com/Wikid82/go_notify_yourself"
	"github.com/Wikid82/go_notify_yourself/transport"
)

func TestRegister_NewReturnsWorkingSender(t *testing.T) {
	w := transport.NewWrapper()

	sender, err := notify.New("pushover", map[string]any{
		"transport": w,
		"user_key":  "u123",
		"api_token": "a456",
	})
	if err != nil {
		t.Fatalf("notify.New(\"pushover\", ...) returned error: %v", err)
	}

	client, ok := sender.(*Client)
	if !ok {
		t.Fatalf("expected *pushover.Client, got %T", sender)
	}
	if client.cfg.UserKey != "u123" || client.cfg.APIToken != "a456" {
		t.Errorf("expected config to be threaded through, got %#v", client.cfg)
	}
}

func TestRegister_MissingTransportReturnsErrorNotPanic(t *testing.T) {
	sender, err := notify.New("pushover", map[string]any{
		"user_key":  "u123",
		"api_token": "a456",
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
		if name == "pushover" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected %q registered in notify.RegisteredTypes(), got %v", "pushover", notify.RegisteredTypes())
	}
}
