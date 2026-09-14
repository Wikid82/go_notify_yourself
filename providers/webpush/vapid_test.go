package webpush

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestGenerateVAPIDKeyPair_ProducesMatchingPair(t *testing.T) {
	pubB64, privB64, err := GenerateVAPIDKeyPair()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	privRaw, err := base64.RawURLEncoding.DecodeString(privB64)
	if err != nil {
		t.Fatalf("private key is not valid base64url: %v", err)
	}
	if len(privRaw) != 32 {
		t.Fatalf("expected 32-byte private key, got %d bytes", len(privRaw))
	}

	pubRaw, err := base64.RawURLEncoding.DecodeString(pubB64)
	if err != nil {
		t.Fatalf("public key is not valid base64url: %v", err)
	}
	if len(pubRaw) != 65 || pubRaw[0] != 0x04 {
		t.Fatalf("expected a 65-byte uncompressed P-256 point, got %d bytes (first byte %#x)", len(pubRaw), pubRaw[0])
	}

	priv, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), privRaw)
	if err != nil {
		t.Fatalf("failed to parse generated private key: %v", err)
	}
	derivedPub, err := priv.PublicKey.Bytes()
	if err != nil {
		t.Fatalf("failed to encode derived public key: %v", err)
	}
	if base64.RawURLEncoding.EncodeToString(derivedPub) != pubB64 {
		t.Fatalf("public key does not match the one derived from the private key")
	}
}

func TestGenerateVAPIDKeyPair_ProducesDistinctKeysEachCall(t *testing.T) {
	pub1, priv1, err := GenerateVAPIDKeyPair()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pub2, priv2, err := GenerateVAPIDKeyPair()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub1 == pub2 || priv1 == priv2 {
		t.Fatalf("expected distinct keypairs across calls, got identical values")
	}
}

func TestBuildVAPIDHeader_ProducesValidSignedJWT(t *testing.T) {
	pubB64, privB64, err := GenerateVAPIDKeyPair()
	if err != nil {
		t.Fatalf("unexpected error generating keypair: %v", err)
	}

	before := time.Now()
	header, err := buildVAPIDHeader(pubB64, privB64, "mailto:ops@example.com", "https://push.example.net/subscription/abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const prefix = "vapid t="
	if !strings.HasPrefix(header, prefix) {
		t.Fatalf("expected header to start with %q, got %q", prefix, header)
	}
	rest := strings.TrimPrefix(header, prefix)
	parts := strings.SplitN(rest, ", k=", 2)
	if len(parts) != 2 {
		t.Fatalf("expected header to contain ', k=', got %q", header)
	}
	jwt, k := parts[0], parts[1]
	if k != pubB64 {
		t.Fatalf("expected k=%q, got %q", pubB64, k)
	}

	jwtParts := strings.Split(jwt, ".")
	if len(jwtParts) != 3 {
		t.Fatalf("expected a 3-part JWT, got %d parts: %q", len(jwtParts), jwt)
	}
	headerB64, payloadB64, sigB64 := jwtParts[0], jwtParts[1], jwtParts[2]

	headerJSON, err := base64.RawURLEncoding.DecodeString(headerB64)
	if err != nil {
		t.Fatalf("JWT header is not valid base64url: %v", err)
	}
	var hdr struct {
		Typ string `json:"typ"`
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(headerJSON, &hdr); err != nil {
		t.Fatalf("JWT header is not valid JSON: %v", err)
	}
	if hdr.Typ != "JWT" || hdr.Alg != "ES256" {
		t.Fatalf("unexpected JWT header: %+v", hdr)
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		t.Fatalf("JWT payload is not valid base64url: %v", err)
	}
	var payload struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Fatalf("JWT payload is not valid JSON: %v", err)
	}
	if payload.Aud != "https://push.example.net" {
		t.Fatalf("expected aud to be the endpoint's scheme+host, got %q", payload.Aud)
	}
	if payload.Sub != "mailto:ops@example.com" {
		t.Fatalf("expected sub to be the configured VAPID subject, got %q", payload.Sub)
	}
	wantExp := before.Add(vapidJWTLifetime)
	gotExp := time.Unix(payload.Exp, 0)
	if diff := gotExp.Sub(wantExp); diff < -5*time.Second || diff > 5*time.Second {
		t.Fatalf("expected exp close to %v, got %v (diff %v)", wantExp, gotExp, diff)
	}

	sigRaw, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		t.Fatalf("JWT signature is not valid base64url: %v", err)
	}
	if len(sigRaw) != 64 {
		t.Fatalf("expected a 64-byte raw r||s signature, got %d bytes", len(sigRaw))
	}
	r := new(big.Int).SetBytes(sigRaw[:32])
	s := new(big.Int).SetBytes(sigRaw[32:])

	pubRaw, err := base64.RawURLEncoding.DecodeString(pubB64)
	if err != nil {
		t.Fatalf("public key is not valid base64url: %v", err)
	}
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), pubRaw)
	if err != nil {
		t.Fatalf("failed to parse public key: %v", err)
	}

	digest := sha256.Sum256([]byte(headerB64 + "." + payloadB64))
	if !ecdsa.Verify(pub, digest[:], r, s) {
		t.Fatalf("signature does not verify against the configured VAPID public key")
	}
}

func TestBuildVAPIDHeader_RejectsInvalidPrivateKey(t *testing.T) {
	pubB64, _, err := GenerateVAPIDKeyPair()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := buildVAPIDHeader(pubB64, "not-valid-base64url!!", "mailto:ops@example.com", "https://push.example.net/x"); err == nil {
		t.Fatal("expected an error for an invalid private key")
	}
}

func TestBuildVAPIDHeader_RejectsInvalidEndpoint(t *testing.T) {
	pubB64, privB64, err := GenerateVAPIDKeyPair()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := buildVAPIDHeader(pubB64, privB64, "mailto:ops@example.com", "://not-a-url"); err == nil {
		t.Fatal("expected an error for an unparsable endpoint")
	}
}
