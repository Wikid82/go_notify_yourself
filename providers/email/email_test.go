package email

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	notify "github.com/Wikid82/go_notify_yourself"
)

type fakeMailer struct {
	recipients []string
	subject    string
	htmlBody   string
	err        error
	calls      int
}

func (f *fakeMailer) Send(_ context.Context, recipients []string, subject, htmlBody string) error {
	f.calls++
	f.recipients = recipients
	f.subject = subject
	f.htmlBody = htmlBody
	return f.err
}

type fakeRenderer struct {
	templateName string
	msg          notify.Message
	htmlBody     string
	err          error
}

func (f *fakeRenderer) Render(templateName string, msg notify.Message) (string, error) {
	f.templateName = templateName
	f.msg = msg
	if f.err != nil {
		return "", f.err
	}
	return f.htmlBody, nil
}

func TestClientSendRequiresMailer(t *testing.T) {
	client := New(Config{Recipients: []string{"a@example.com"}})
	err := client.Send(context.Background(), notify.Message{Title: "t"})
	if err == nil || !strings.Contains(err.Error(), "no Mailer configured") {
		t.Fatalf("expected no-Mailer error, got: %v", err)
	}
}

func TestClientSendRequiresRecipients(t *testing.T) {
	client := New(Config{Mailer: &fakeMailer{}})
	err := client.Send(context.Background(), notify.Message{Title: "t"})
	if err == nil || !strings.Contains(err.Error(), "no recipients configured") {
		t.Fatalf("expected no-recipients error, got: %v", err)
	}
}

func TestClientSendFiltersBlankRecipients(t *testing.T) {
	mailer := &fakeMailer{}
	client := New(Config{Recipients: []string{" ", "", "a@example.com", "  b@example.com  "}, Mailer: mailer})

	if err := client.Send(context.Background(), notify.Message{Title: "t", Body: "body"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mailer.recipients) != 2 || mailer.recipients[0] != "a@example.com" || mailer.recipients[1] != "b@example.com" {
		t.Fatalf("expected filtered/trimmed recipients, got %#v", mailer.recipients)
	}
}

func TestClientSendUsesSubjectPrefix(t *testing.T) {
	mailer := &fakeMailer{}
	client := New(Config{Recipients: []string{"a@example.com"}, Mailer: mailer, SubjectPrefix: "[MyApp] "})

	if err := client.Send(context.Background(), notify.Message{Title: "Disk full"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mailer.subject != "[MyApp] Disk full" {
		t.Fatalf("expected prefixed subject, got %q", mailer.subject)
	}
}

func TestClientSendNoSubjectPrefixByDefault(t *testing.T) {
	mailer := &fakeMailer{}
	client := New(Config{Recipients: []string{"a@example.com"}, Mailer: mailer})

	if err := client.Send(context.Background(), notify.Message{Title: "Disk full"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mailer.subject != "Disk full" {
		t.Fatalf("expected unprefixed subject by default, got %q", mailer.subject)
	}
}

func TestClientSendSanitizesControlCharacters(t *testing.T) {
	mailer := &fakeMailer{}
	client := New(Config{Recipients: []string{"a@example.com"}, Mailer: mailer})

	err := client.Send(context.Background(), notify.Message{Title: "bad\x00title", Body: "bad\x07body"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.ContainsAny(mailer.subject, "\x00\x07") {
		t.Fatalf("expected control characters stripped from subject, got %q", mailer.subject)
	}
}

func TestClientSendUsesCustomTemplateNameSelector(t *testing.T) {
	renderer := &fakeRenderer{htmlBody: "<p>hi</p>"}
	mailer := &fakeMailer{}
	client := New(Config{
		Recipients: []string{"a@example.com"},
		Mailer:     mailer,
		Renderer:   renderer,
		TemplateName: func(msg notify.Message) string {
			return "custom-" + msg.EventType
		},
	})

	if err := client.Send(context.Background(), notify.Message{Title: "t", EventType: "cert"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if renderer.templateName != "custom-cert" {
		t.Fatalf("expected TemplateName selector used, got %q", renderer.templateName)
	}
	if mailer.htmlBody != "<p>hi</p>" {
		t.Fatalf("expected custom renderer output used as email body, got %q", mailer.htmlBody)
	}
}

func TestClientSendDefaultTemplateNameIsDefault(t *testing.T) {
	renderer := &fakeRenderer{htmlBody: "<p>hi</p>"}
	client := New(Config{Recipients: []string{"a@example.com"}, Mailer: &fakeMailer{}, Renderer: renderer})

	if err := client.Send(context.Background(), notify.Message{Title: "t"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if renderer.templateName != "default" {
		t.Fatalf("expected default template name, got %q", renderer.templateName)
	}
}

func TestClientSendPropagatesRendererError(t *testing.T) {
	renderer := &fakeRenderer{err: fmt.Errorf("boom")}
	client := New(Config{Recipients: []string{"a@example.com"}, Mailer: &fakeMailer{}, Renderer: renderer})

	err := client.Send(context.Background(), notify.Message{Title: "t"})
	if err == nil || !strings.Contains(err.Error(), "render template") {
		t.Fatalf("expected wrapped render error, got: %v", err)
	}
}

func TestClientSendPropagatesMailerError(t *testing.T) {
	mailer := &fakeMailer{err: fmt.Errorf("smtp down")}
	client := New(Config{Recipients: []string{"a@example.com"}, Mailer: mailer})

	err := client.Send(context.Background(), notify.Message{Title: "t"})
	if err == nil || !strings.Contains(err.Error(), "smtp down") {
		t.Fatalf("expected wrapped mailer error, got: %v", err)
	}
}

func TestClientSendUsesDefaultRendererWhenNoneConfigured(t *testing.T) {
	mailer := &fakeMailer{}
	client := New(Config{Recipients: []string{"a@example.com"}, Mailer: mailer})

	if err := client.Send(context.Background(), notify.Message{Title: "Hello", Body: "World"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(mailer.htmlBody, "Hello") || !strings.Contains(mailer.htmlBody, "World") {
		t.Fatalf("expected default template to render title/body, got %q", mailer.htmlBody)
	}
	// The default template is neutral/unbranded by design (see
	// defaultHTML in default_template.go): no logo, no product name,
	// nothing beyond the caller-supplied Title/Body/EventType/Timestamp.
}

func TestSanitizeForEmail(t *testing.T) {
	got := sanitizeForEmail("  hello\x00world\x7F  ")
	if got != "helloworld" {
		t.Fatalf("expected control chars stripped and trimmed, got %q", got)
	}
}

func TestDefaultTemplateRendererEscapesHTML(t *testing.T) {
	r := defaultTemplateRenderer{}
	html, err := r.Render("default", notify.Message{Title: "<script>alert(1)</script>", Body: "safe"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(html, "<script>") {
		t.Fatalf("expected HTML-escaped title, got %q", html)
	}
}

func TestDefaultTemplateRendererOmitsEmptyFields(t *testing.T) {
	r := defaultTemplateRenderer{}
	html, err := r.Render("default", notify.Message{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(html, "<h2") {
		t.Fatalf("expected no title element for empty title, got %q", html)
	}
}

func TestDefaultTemplateRendererIncludesEventTypeAndTimestamp(t *testing.T) {
	r := defaultTemplateRenderer{}
	ts := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	html, err := r.Render("default", notify.Message{Title: "t", EventType: "cert", Timestamp: ts})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(html, "cert") || !strings.Contains(html, "2024-05-01T12:00:00Z") {
		t.Fatalf("expected event type and formatted timestamp present, got %q", html)
	}
}
