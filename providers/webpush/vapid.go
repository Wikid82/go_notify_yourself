package webpush

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	neturl "net/url"
	"time"
)

// vapidJWTLifetime bounds the "exp" claim on the VAPID JWT Send signs for
// each request: 12 hours from the time of signing. RFC 8292 recommends an
// expiration no more than 24 hours out; 12 hours is comfortably inside
// that bound while still meaning a Client's signed header is reusable
// across a short burst of retries/sends without re-signing every time
// (though Send always signs fresh per call — see below).
const vapidJWTLifetime = 12 * time.Hour

// vapidJWTHeader is the fixed RFC 8292 JOSE header — every VAPID JWT this
// module signs uses ES256, so this is not templated per-request.
var vapidJWTHeaderJSON = mustMarshalJSON(map[string]string{"typ": "JWT", "alg": "ES256"})

func mustMarshalJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("webpush: failed to marshal fixed JWT header: %v", err))
	}
	return b
}

// buildVAPIDHeader builds the RFC 8292 "Authorization: vapid t=<jwt>,
// k=<public key>" header value for a request to endpoint, signed with the
// given VAPID keypair/subject. aud is derived from endpoint's scheme+host
// (RFC 8292 §2: the JWT audience is the push service's origin, not the
// full subscription path).
//
// Takes the three VAPID strings directly rather than the whole Config by
// design, not just convenience: buildVAPIDHeader only ever reads 3 of
// Config's 11 fields, and Config itself isn't defined until webpush.go —
// a Config parameter here would make this file depend on a type that
// doesn't exist yet at this point in the commit sequence, breaking this
// module's per-commit build/test guarantee. Taking plain strings keeps
// this function buildable and independently testable two commits before
// Config exists.
func buildVAPIDHeader(vapidPublicKey, vapidPrivateKey, vapidSubject, endpoint string) (string, error) {
	privRaw, err := base64.RawURLEncoding.DecodeString(vapidPrivateKey)
	if err != nil {
		return "", fmt.Errorf("webpush: VAPID private key: invalid base64url encoding: %w", err)
	}
	priv, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), privRaw)
	if err != nil {
		return "", fmt.Errorf("webpush: VAPID private key: %w", err)
	}

	parsedEndpoint, err := neturl.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("webpush: parse endpoint for VAPID audience: %w", err)
	}
	aud := parsedEndpoint.Scheme + "://" + parsedEndpoint.Host

	payloadJSON, err := json.Marshal(struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}{
		Aud: aud,
		Exp: time.Now().Add(vapidJWTLifetime).Unix(),
		Sub: vapidSubject,
	})
	if err != nil {
		return "", fmt.Errorf("webpush: marshal VAPID JWT payload: %w", err)
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(vapidJWTHeaderJSON)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)

	digest := sha256.Sum256([]byte(headerB64 + "." + payloadB64))
	r, s, err := ecdsa.Sign(rand.Reader, priv, digest[:])
	if err != nil {
		return "", fmt.Errorf("webpush: sign VAPID JWT: %w", err)
	}

	sig := make([]byte, 64)
	r.FillBytes(sig[0:32])
	s.FillBytes(sig[32:64])
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return fmt.Sprintf("vapid t=%s.%s.%s, k=%s", headerB64, payloadB64, sigB64, vapidPublicKey), nil
}

// GenerateVAPIDKeyPair generates a new P-256 VAPID application server
// keypair, returned as the same base64url (no padding) encoded strings
// Config.VAPIDPublicKey/Config.VAPIDPrivateKey expect. Intended to be
// called once at application setup time (e.g. from an init/CLI flow) and
// the results persisted by the host application — every browser
// PushSubscription is bound to the exact public key it was created with
// (PushManager.subscribe({applicationServerKey: ...})), so rotating this
// keypair invalidates every existing subscription the host has collected.
// privateKey is a credential, not a diagnostic value: callers must not log
// it (a mistake this function can't prevent, only warn against — the same
// discipline hosts already need for VAPIDPrivateKey once it's in Config).
func GenerateVAPIDKeyPair() (publicKey, privateKey string, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("webpush: generate VAPID keypair: %w", err)
	}

	pubBytes, err := priv.PublicKey.Bytes()
	if err != nil {
		return "", "", fmt.Errorf("webpush: encode VAPID public key: %w", err)
	}
	privBytes, err := priv.Bytes()
	if err != nil {
		return "", "", fmt.Errorf("webpush: encode VAPID private key: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(pubBytes), base64.RawURLEncoding.EncodeToString(privBytes), nil
}
