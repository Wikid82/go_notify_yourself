// Package webpush implements notify.Sender for direct browser Web Push
// delivery (RFC 8030/8291/8292) — no third-party relay involved. A single
// Config pairs one application's VAPID identity with one browser
// PushSubscription; a host application fanning a Message out to many
// subscribers constructs one *Client per subscription (cheap: New does no
// I/O) and calls Send on each, exactly like fanning out to many
// Sender values of any other provider type.
package webpush

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	notify "github.com/Wikid82/go_notify_yourself"
	render "github.com/Wikid82/go_notify_yourself/providers/internal/render"
	"github.com/Wikid82/go_notify_yourself/transport"
)

// DefaultTTL is used for the RFC 8030 "TTL" header when Config.TTL is zero.
// Four weeks (2,419,200 seconds) — a conservative value inside the maximum
// retention window most push services honor before evicting an
// undelivered message. See Config.TTL's doc comment for why Config.TTL's
// zero value is *not* treated as RFC 8030's spec-legal "attempt immediate
// delivery only, don't store" meaning.
const DefaultTTL = 4 * 7 * 24 * 3600

// topicPattern matches RFC 8030's "Topic" header charset: up to 32
// characters from the URL-and-filename-safe base64 alphabet.
var topicPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// Config configures a webpush Sender. Unlike every other provider's Config,
// this one mixes two conceptually distinct groups of fields: VAPID
// application identity (shared across every subscription this application
// pushes to) and one subscriber's PushSubscription destination. A host
// application constructs one webpush.Client per subscriber, reusing the
// same VAPID* values across all of them — see the package doc comment for
// the fan-out pattern.
type Config struct {
	// --- VAPID application identity (RFC 8292) ---

	// VAPIDPublicKey is the application server's VAPID public key: an
	// uncompressed P-256 point (65 bytes: 0x04 || X || Y), base64url
	// (no padding) encoded. This is the same value the browser is given as
	// PushManager.subscribe({applicationServerKey: VAPIDPublicKey}).
	// Required.
	VAPIDPublicKey string

	// VAPIDPrivateKey is the application server's VAPID private key: a
	// 32-byte P-256 scalar, base64url (no padding) encoded. Required. This
	// value never leaves the process — Send signs a JWT with it locally
	// and never transmits it.
	VAPIDPrivateKey string

	// VAPIDSubject identifies the application server operator, per RFC
	// 8292's "sub" JWT claim: a "mailto:" or "https:" URI (e.g.
	// "mailto:ops@example.com"). Some push services (notably Mozilla's)
	// reject a VAPID JWT with an empty or malformed sub. Required.
	VAPIDSubject string

	// --- Subscriber destination (the browser's PushSubscription) ---

	// Endpoint is the subscription's push service URL, from
	// PushSubscription.endpoint. Required.
	Endpoint string

	// P256dh is the subscriber's P-256 Diffie-Hellman public key, from
	// PushSubscription.getKey('p256dh'): base64url (no padding) encoded.
	// Required.
	P256dh string

	// Auth is the subscriber's 16-byte authentication secret, from
	// PushSubscription.getKey('auth'): base64url (no padding) encoded.
	// Required.
	Auth string

	// --- Delivery hints (RFC 8030) ---

	// TTL is the number of seconds the push service should retain the
	// message if the subscriber is currently offline, sent as the "TTL"
	// header. Zero uses DefaultTTL — see that constant's doc comment.
	TTL int

	// Urgency is an optional RFC 8030 "Urgency" header value: one of
	// "very-low", "low", "normal", "high". Empty omits the header (the
	// push service's own default applies, typically "normal"). Send
	// rejects any other value.
	Urgency string

	// Topic is an optional RFC 8030 "Topic" header value: up to 32
	// characters from the URL-and-filename-safe base64 alphabet
	// ([A-Za-z0-9_-]). When set, a pending undelivered message with the
	// same Topic is replaced rather than queued alongside it. Empty omits
	// the header. Send rejects a Topic outside this charset/length.
	Topic string

	// --- Payload templating ---

	// Template selects the JSON payload shape: "minimal" (default),
	// "detailed", or "custom" (uses CustomTemplate) — same convention as
	// every other JSON-payload provider (providers/internal/render). The
	// rendered JSON is the plaintext that gets RFC 8291-encrypted; the
	// receiving service worker's `push` event handler is responsible for
	// JSON.parse-ing the decrypted payload. This module has no opinion on
	// what the service worker does with it beyond that it is valid JSON.
	Template string

	// CustomTemplate is a user-supplied Go text/template string, used when
	// Template is "custom".
	CustomTemplate string
}

// Client dispatches notify.Message values to one browser PushSubscription.
type Client struct {
	cfg     Config
	wrapper *transport.Wrapper
}

var _ notify.Sender = (*Client)(nil)

// New constructs a webpush Client. w performs the actual dispatch — see
// transport.NewWrapper.
func New(cfg Config, w *transport.Wrapper) *Client {
	return &Client{cfg: cfg, wrapper: w}
}

// verifyVAPIDKeyPairMatches decodes vapidPrivateKey, derives its
// corresponding public key, and compares it byte-for-byte against
// vapidPublicKey — catching a copy-pasted mismatched keypair locally
// instead of deferring it to an opaque remote 401/403 from the push
// service.
func verifyVAPIDKeyPairMatches(vapidPublicKey, vapidPrivateKey string) error {
	privRaw, err := base64.RawURLEncoding.DecodeString(vapidPrivateKey)
	if err != nil {
		return fmt.Errorf("webpush: VAPID private key: invalid base64url encoding: %w", err)
	}
	priv, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), privRaw)
	if err != nil {
		return fmt.Errorf("webpush: VAPID private key: %w", err)
	}
	derivedPub, err := priv.PublicKey.Bytes()
	if err != nil {
		return fmt.Errorf("webpush: VAPID private key: %w", err)
	}

	pubRaw, err := base64.RawURLEncoding.DecodeString(vapidPublicKey)
	if err != nil {
		return fmt.Errorf("webpush: VAPID public key: invalid base64url encoding: %w", err)
	}

	if !bytes.Equal(derivedPub, pubRaw) {
		return fmt.Errorf("webpush: VAPID public/private key pair does not match")
	}
	return nil
}

// Send renders msg using the configured template, RFC 8291-encrypts the
// result for the configured subscriber, and dispatches it to
// cfg.Endpoint via the shared transport.Wrapper, authenticated with an
// RFC 8292 VAPID JSON Web Token signed for this request.
func (c *Client) Send(ctx context.Context, msg notify.Message) error {
	vapidPublicKey := strings.TrimSpace(c.cfg.VAPIDPublicKey)
	if vapidPublicKey == "" {
		return fmt.Errorf("webpush: VAPID public key is not configured")
	}
	vapidPrivateKey := strings.TrimSpace(c.cfg.VAPIDPrivateKey)
	if vapidPrivateKey == "" {
		return fmt.Errorf("webpush: VAPID private key is not configured")
	}
	vapidSubject := strings.TrimSpace(c.cfg.VAPIDSubject)
	if vapidSubject == "" {
		return fmt.Errorf("webpush: VAPID subject is not configured")
	}
	if !strings.HasPrefix(vapidSubject, "mailto:") && !strings.HasPrefix(vapidSubject, "https://") {
		return fmt.Errorf("webpush: VAPID subject must start with %q or %q", "mailto:", "https://")
	}
	endpoint := strings.TrimSpace(c.cfg.Endpoint)
	if endpoint == "" {
		return fmt.Errorf("webpush: endpoint is not configured")
	}
	p256dh := strings.TrimSpace(c.cfg.P256dh)
	if p256dh == "" {
		return fmt.Errorf("webpush: p256dh is not configured")
	}
	auth := strings.TrimSpace(c.cfg.Auth)
	if auth == "" {
		return fmt.Errorf("webpush: auth is not configured")
	}

	if err := verifyVAPIDKeyPairMatches(vapidPublicKey, vapidPrivateKey); err != nil {
		return err
	}

	urgency := strings.TrimSpace(c.cfg.Urgency)
	if urgency != "" {
		switch urgency {
		case "very-low", "low", "normal", "high":
		default:
			return fmt.Errorf("webpush: invalid urgency %q", urgency)
		}
	}

	topic := strings.TrimSpace(c.cfg.Topic)
	if topic != "" && !topicPattern.MatchString(topic) {
		return fmt.Errorf("webpush: invalid topic %q: must be 1-32 URL-safe base64 characters", topic)
	}

	tmplStr := render.SelectTemplate(c.cfg.Template, c.cfg.CustomTemplate, render.MinimalTemplate, render.DetailedTemplate)
	rendered, err := render.Render(tmplStr, render.TemplateData(msg))
	if err != nil {
		return err
	}

	var payload any
	if err := json.Unmarshal([]byte(rendered), &payload); err != nil {
		return fmt.Errorf("invalid JSON payload: %w", err)
	}

	ciphertext, err := encryptAES128GCM(p256dh, auth, []byte(rendered))
	if err != nil {
		return fmt.Errorf("webpush: encrypt payload: %w", err)
	}

	authHeader, err := buildVAPIDHeader(vapidPublicKey, vapidPrivateKey, vapidSubject, endpoint)
	if err != nil {
		return fmt.Errorf("webpush: build VAPID header: %w", err)
	}

	ttl := c.cfg.TTL
	if ttl == 0 {
		ttl = DefaultTTL
	}

	headers := map[string]string{
		"Content-Type":     "application/octet-stream",
		"Content-Encoding": "aes128gcm",
		"Authorization":    authHeader,
		"TTL":              strconv.Itoa(ttl),
	}
	if urgency != "" {
		headers["Urgency"] = urgency
	}
	if topic != "" {
		headers["Topic"] = topic
	}

	if _, err := c.wrapper.Send(ctx, transport.Request{
		URL:     endpoint,
		Headers: headers,
		Body:    ciphertext,
	}); err != nil {
		return fmt.Errorf("failed to send web push: %w", err)
	}

	return nil
}
