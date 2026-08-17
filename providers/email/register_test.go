package email

import (
	"reflect"
	"testing"

	notify "github.com/Wikid82/go_notify_yourself"
)

func TestRegister_NewReturnsWorkingSender(t *testing.T) {
	mailer := &fakeMailer{}

	sender, err := notify.New("email", map[string]any{
		"mailer":         mailer,
		"recipients":     []string{"ops@example.com"},
		"subject_prefix": "[MyApp] ",
	})
	if err != nil {
		t.Fatalf("notify.New(\"email\", ...) returned error: %v", err)
	}

	client, ok := sender.(*Client)
	if !ok {
		t.Fatalf("expected *email.Client, got %T", sender)
	}
	if client.cfg.Mailer != mailer {
		t.Errorf("expected Mailer to be threaded through from config")
	}
	if !reflect.DeepEqual(client.cfg.Recipients, []string{"ops@example.com"}) {
		t.Errorf("expected Recipients to be threaded through, got %#v", client.cfg.Recipients)
	}
	if client.cfg.SubjectPrefix != "[MyApp] " {
		t.Errorf("expected SubjectPrefix to be threaded through, got %q", client.cfg.SubjectPrefix)
	}
}

func TestRegister_MissingMailerReturnsErrorNotPanic(t *testing.T) {
	sender, err := notify.New("email", map[string]any{
		"recipients": []string{"ops@example.com"},
	})
	if err == nil {
		t.Fatal("expected an error when config[\"mailer\"] is missing")
	}
	if sender != nil {
		t.Fatalf("expected a nil Sender on error, got %#v", sender)
	}
}

func TestRegister_WrongTypedMailerReturnsError(t *testing.T) {
	_, err := notify.New("email", map[string]any{
		"mailer": "not-a-mailer",
	})
	if err == nil {
		t.Fatal("expected an error when config[\"mailer\"] is the wrong type")
	}
}

func TestRegister_OptionalRendererAndTemplateNamePassThrough(t *testing.T) {
	mailer := &fakeMailer{}
	renderer := &fakeRenderer{htmlBody: "<p>hi</p>"}
	templateNameFn := func(notify.Message) string { return "custom-template" }

	sender, err := notify.New("email", map[string]any{
		"mailer":        mailer,
		"recipients":    []string{"ops@example.com"},
		"renderer":      renderer,
		"template_name": templateNameFn,
	})
	if err != nil {
		t.Fatalf("notify.New(\"email\", ...) returned error: %v", err)
	}

	client, ok := sender.(*Client)
	if !ok {
		t.Fatalf("expected *email.Client, got %T", sender)
	}
	if client.cfg.Renderer != renderer {
		t.Errorf("expected Renderer to be threaded through from config")
	}
	if client.cfg.TemplateName == nil || client.cfg.TemplateName(notify.Message{}) != "custom-template" {
		t.Errorf("expected TemplateName closure to be threaded through from config")
	}
}

func TestRegister_RegisteredUnderExpectedName(t *testing.T) {
	found := false
	for _, name := range notify.RegisteredTypes() {
		if name == "email" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected %q registered in notify.RegisteredTypes(), got %v", "email", notify.RegisteredTypes())
	}
}
