package webpush

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	notify "github.com/Wikid82/go_notify_yourself"
	"github.com/Wikid82/go_notify_yourself/transport"
)

type capturingRoundTripper struct {
	lastRequest *http.Request
	lastBody    []byte
	statusCode  int
	respBody    string
}

func (c *capturingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	c.lastRequest = req
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		c.lastBody = b
	}
	status := c.statusCode
	if status == 0 {
		status = http.StatusCreated
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(c.respBody)),
		Header:     make(http.Header),
	}, nil
}

func passthroughValidator(rawURL string, _ bool) (string, error) { return rawURL, nil }

func newTestWrapper(rt *capturingRoundTripper) *transport.Wrapper {
	return transport.NewWrapper(
		transport.WithURLValidator(passthroughValidator),
		transport.WithClientFactory(func(bool, int) *http.Client {
			return &http.Client{Transport: rt}
		}),
		transport.WithRetryPolicy(transport.RetryPolicy{MaxAttempts: 1}),
	)
}

const validEndpoint = "https://push.example.net/subscription/abc123"

// testSubscriber generates a fresh, valid subscriber p256dh/auth pair for
// tests that need to reach the encryption step successfully.
func testSubscriber(t *testing.T) (p256dh, auth string) {
	t.Helper()
	receiver, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("unexpected keygen error: %v", err)
	}
	authSecret := make([]byte, 16)
	if _, err := rand.Read(authSecret); err != nil {
		t.Fatalf("unexpected rand error: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(receiver.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(authSecret)
}

// validConfig returns a Config with a genuinely matching VAPID keypair and
// a valid subscriber, suitable as a base for tests that mutate one field.
func validConfig(t *testing.T) Config {
	t.Helper()
	pub, priv, err := GenerateVAPIDKeyPair()
	if err != nil {
		t.Fatalf("unexpected error generating VAPID keypair: %v", err)
	}
	p256dh, auth := testSubscriber(t)
	return Config{
		VAPIDPublicKey:  pub,
		VAPIDPrivateKey: priv,
		VAPIDSubject:    "mailto:ops@example.com",
		Endpoint:        validEndpoint,
		P256dh:          p256dh,
		Auth:            auth,
	}
}

func TestClientSend_RejectsMissingVAPIDPublicKey(t *testing.T) {
	cfg := validConfig(t)
	cfg.VAPIDPublicKey = ""
	rt := &capturingRoundTripper{}
	client := New(cfg, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "is not configured") {
		t.Fatalf("expected not-configured error, got: %v", err)
	}
}

func TestClientSend_RejectsMissingVAPIDPrivateKey(t *testing.T) {
	cfg := validConfig(t)
	cfg.VAPIDPrivateKey = ""
	rt := &capturingRoundTripper{}
	client := New(cfg, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "is not configured") {
		t.Fatalf("expected not-configured error, got: %v", err)
	}
}

func TestClientSend_RejectsMissingVAPIDSubject(t *testing.T) {
	cfg := validConfig(t)
	cfg.VAPIDSubject = ""
	rt := &capturingRoundTripper{}
	client := New(cfg, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "is not configured") {
		t.Fatalf("expected not-configured error, got: %v", err)
	}
}

func TestClientSend_RejectsMalformedVAPIDSubjectPrefix(t *testing.T) {
	cfg := validConfig(t)
	cfg.VAPIDSubject = "ops@example.com"
	rt := &capturingRoundTripper{}
	client := New(cfg, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "must start with") {
		t.Fatalf("expected VAPID subject prefix error, got: %v", err)
	}
}

func TestClientSend_AllowsHTTPSVAPIDSubject(t *testing.T) {
	cfg := validConfig(t)
	cfg.VAPIDSubject = "https://example.com/contact"
	rt := &capturingRoundTripper{}
	client := New(cfg, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Body: "hello"}); err != nil {
		t.Fatalf("unexpected error with https: VAPID subject: %v", err)
	}
}

func TestClientSend_RejectsMissingEndpoint(t *testing.T) {
	cfg := validConfig(t)
	cfg.Endpoint = ""
	rt := &capturingRoundTripper{}
	client := New(cfg, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "is not configured") {
		t.Fatalf("expected not-configured error, got: %v", err)
	}
}

func TestClientSend_RejectsMissingP256dh(t *testing.T) {
	cfg := validConfig(t)
	cfg.P256dh = ""
	rt := &capturingRoundTripper{}
	client := New(cfg, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "is not configured") {
		t.Fatalf("expected not-configured error, got: %v", err)
	}
}

func TestClientSend_RejectsMissingAuth(t *testing.T) {
	cfg := validConfig(t)
	cfg.Auth = ""
	rt := &capturingRoundTripper{}
	client := New(cfg, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "is not configured") {
		t.Fatalf("expected not-configured error, got: %v", err)
	}
}

func TestClientSend_RejectsMismatchedVAPIDKeyPair(t *testing.T) {
	cfg := validConfig(t)
	otherPub, _, err := GenerateVAPIDKeyPair()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg.VAPIDPublicKey = otherPub
	rt := &capturingRoundTripper{}
	client := New(cfg, newTestWrapper(rt))

	err = client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected VAPID key pair mismatch error, got: %v", err)
	}
}

func TestClientSend_RejectsMalformedVAPIDPrivateKeyBase64(t *testing.T) {
	cfg := validConfig(t)
	cfg.VAPIDPrivateKey = "not valid base64!!!"
	rt := &capturingRoundTripper{}
	client := New(cfg, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "VAPID private key") {
		t.Fatalf("expected VAPID private key decode error, got: %v", err)
	}
}

func TestClientSend_RejectsMalformedVAPIDPublicKeyBase64(t *testing.T) {
	cfg := validConfig(t)
	cfg.VAPIDPublicKey = "not valid base64!!!"
	rt := &capturingRoundTripper{}
	client := New(cfg, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "VAPID public key") {
		t.Fatalf("expected VAPID public key decode error, got: %v", err)
	}
}

func TestClientSend_RejectsInvalidUrgency(t *testing.T) {
	cfg := validConfig(t)
	cfg.Urgency = "extremely-urgent"
	rt := &capturingRoundTripper{}
	client := New(cfg, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "invalid urgency") {
		t.Fatalf("expected invalid urgency error, got: %v", err)
	}
}

func TestClientSend_RejectsInvalidTopic(t *testing.T) {
	cfg := validConfig(t)
	cfg.Topic = "not a valid topic!"
	rt := &capturingRoundTripper{}
	client := New(cfg, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "invalid topic") {
		t.Fatalf("expected invalid topic error, got: %v", err)
	}
}

func TestClientSend_RejectsNonJSONTemplateOutput(t *testing.T) {
	cfg := validConfig(t)
	cfg.Template = "custom"
	cfg.CustomTemplate = "not json"
	rt := &capturingRoundTripper{}
	client := New(cfg, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "invalid JSON payload") {
		t.Fatalf("expected invalid JSON payload error, got: %v", err)
	}
}

func TestClientSend_DoesNotRequireMessageField(t *testing.T) {
	cfg := validConfig(t)
	cfg.Template = "custom"
	cfg.CustomTemplate = `{"foo":"bar"}`
	rt := &capturingRoundTripper{}
	client := New(cfg, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Body: "x"}); err != nil {
		t.Fatalf("unexpected error: webpush should not require a 'message' field: %v", err)
	}
}

func TestClientSend_RejectsOversizedPlaintext(t *testing.T) {
	cfg := validConfig(t)
	cfg.Template = "custom"
	cfg.CustomTemplate = `{"message":"` + strings.Repeat("a", MaxPlaintextSize+100) + `"}`
	rt := &capturingRoundTripper{}
	client := New(cfg, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "x"})
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum plaintext size") {
		t.Fatalf("expected oversized plaintext error, got: %v", err)
	}
}

func TestClientSend_SendsCorrectHeadersOnSuccessWithDefaultTTL(t *testing.T) {
	cfg := validConfig(t)
	rt := &capturingRoundTripper{}
	client := New(cfg, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Title: "hi", Body: "hello"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := rt.lastRequest.Header.Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("expected Content-Type application/octet-stream, got %q", got)
	}
	if got := rt.lastRequest.Header.Get("Content-Encoding"); got != "aes128gcm" {
		t.Fatalf("expected Content-Encoding aes128gcm, got %q", got)
	}
	if got := rt.lastRequest.Header.Get("TTL"); got != strconv.Itoa(DefaultTTL) {
		t.Fatalf("expected TTL header %d (DefaultTTL), got %q", DefaultTTL, got)
	}
	auth := rt.lastRequest.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "vapid t=") {
		t.Fatalf("expected vapid Authorization header, got %q", auth)
	}
	if !strings.Contains(auth, "k="+cfg.VAPIDPublicKey) {
		t.Fatalf("expected Authorization header to carry the configured public key, got %q", auth)
	}
}

func TestClientSend_BodyIsCiphertextNotPlaintext(t *testing.T) {
	cfg := validConfig(t)
	cfg.Template = "custom"
	cfg.CustomTemplate = `{"message":"a very secret plaintext value"}`
	rt := &capturingRoundTripper{}
	client := New(cfg, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Body: "x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if bytes.Contains(rt.lastBody, []byte("a very secret plaintext value")) {
		t.Fatalf("request body leaked the plaintext payload: %q", rt.lastBody)
	}
	if len(rt.lastBody) < 86 {
		t.Fatalf("expected an aes128gcm-framed body of at least 86 bytes, got %d", len(rt.lastBody))
	}
}

func TestClientSend_TTLReflectsConfiguredValue(t *testing.T) {
	cfg := validConfig(t)
	cfg.TTL = 3600
	rt := &capturingRoundTripper{}
	client := New(cfg, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Body: "hello"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := rt.lastRequest.Header.Get("TTL"); got != "3600" {
		t.Fatalf("expected TTL header 3600, got %q", got)
	}
}

func TestClientSend_UrgencyAndTopicHeadersOnlyWhenConfigured(t *testing.T) {
	cfg := validConfig(t)
	rt := &capturingRoundTripper{}
	client := New(cfg, newTestWrapper(rt))

	if err := client.Send(context.Background(), notify.Message{Body: "hello"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := rt.lastRequest.Header.Get("Urgency"); got != "" {
		t.Fatalf("expected no Urgency header when unconfigured, got %q", got)
	}
	if got := rt.lastRequest.Header.Get("Topic"); got != "" {
		t.Fatalf("expected no Topic header when unconfigured, got %q", got)
	}

	cfg.Urgency = "high"
	cfg.Topic = "my-topic"
	rt2 := &capturingRoundTripper{}
	client2 := New(cfg, newTestWrapper(rt2))
	if err := client2.Send(context.Background(), notify.Message{Body: "hello"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := rt2.lastRequest.Header.Get("Urgency"); got != "high" {
		t.Fatalf("expected Urgency header 'high', got %q", got)
	}
	if got := rt2.lastRequest.Header.Get("Topic"); got != "my-topic" {
		t.Fatalf("expected Topic header 'my-topic', got %q", got)
	}
}

func TestClientSend_PropagatesWrapperError(t *testing.T) {
	cfg := validConfig(t)
	rt := &capturingRoundTripper{statusCode: http.StatusInternalServerError}
	client := New(cfg, newTestWrapper(rt))

	err := client.Send(context.Background(), notify.Message{Body: "hello"})
	if err == nil || !strings.Contains(err.Error(), "failed to send web push") {
		t.Fatalf("expected wrapped send error, got: %v", err)
	}
}
