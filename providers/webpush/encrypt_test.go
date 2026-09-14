package webpush

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

// RFC 8291 Appendix A ("Intermediate Values for Encryption") fixed test
// vectors, transcribed verbatim (whitespace/line-wrapping removed per the
// RFC's own note that presentation whitespace can be discarded).
const (
	rfc8291UAPublic  = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	rfc8291UAPrivate = "q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94"
	rfc8291ASPublic  = "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8"
	rfc8291ASPrivate = "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"
	rfc8291Salt      = "DGv6ra1nlYgDCS1FRnbzlw"
	rfc8291Auth      = "BTBZMqHH6r4Tts7J_aSIgg"
	rfc8291Plaintext = "When I grow up, I want to be a watermelon"

	// rfc8291ExpectedBody is the exact wire body from RFC 8291 §5 (header
	// || AEAD ciphertext), with the example's line-wrapping removed.
	rfc8291ExpectedBody = "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml" +
		"mlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPT" +
		"pK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
)

func mustDecodeB64URL(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("failed to decode base64url %q: %v", s, err)
	}
	return b
}

// TestEncryptAES128GCMWithKeys_RFC8291AppendixAVector is the single
// highest-value correctness gate in this feature: it feeds the exact
// fixed inputs from RFC 8291 Appendix A into encryptAES128GCMWithKeys and
// asserts the output matches the RFC's published ciphertext byte-for-byte.
// This must not be weakened to a round-trip-only check — a bug symmetric
// in both directions (e.g. a wrong HKDF info string used consistently on
// both sides) would still round-trip correctly while being wrong per the
// RFC, and there is no external Web Push implementation available to
// interop-test against in this dependency-free module.
func TestEncryptAES128GCMWithKeys_RFC8291AppendixAVector(t *testing.T) {
	asPrivateRaw := mustDecodeB64URL(t, rfc8291ASPrivate)
	ephemeral, err := ecdh.P256().NewPrivateKey(asPrivateRaw)
	if err != nil {
		t.Fatalf("failed to construct ephemeral private key from RFC fixture: %v", err)
	}

	salt := mustDecodeB64URL(t, rfc8291Salt)

	got, err := encryptAES128GCMWithKeys(ephemeral, salt, rfc8291UAPublic, rfc8291Auth, []byte(rfc8291Plaintext))
	if err != nil {
		t.Fatalf("encryptAES128GCMWithKeys returned error: %v", err)
	}

	want := mustDecodeB64URL(t, rfc8291ExpectedBody)

	if !bytes.Equal(got, want) {
		t.Fatalf("RFC 8291 Appendix A vector mismatch:\n got  (%d bytes): %s\n want (%d bytes): %s",
			len(got), base64.RawURLEncoding.EncodeToString(got),
			len(want), base64.RawURLEncoding.EncodeToString(want))
	}
}

// TestEncryptAES128GCMWithKeys_RFC8291AppendixAVector_HeaderOnly cross-checks
// the 86-byte RFC 8188 header framing in isolation against Appendix A's
// separately published header value, pinpointing a framing bug distinctly
// from an AEAD/key-derivation bug should the full-body test above fail.
func TestEncryptAES128GCMWithKeys_RFC8291AppendixAVector_HeaderOnly(t *testing.T) {
	asPrivateRaw := mustDecodeB64URL(t, rfc8291ASPrivate)
	ephemeral, err := ecdh.P256().NewPrivateKey(asPrivateRaw)
	if err != nil {
		t.Fatalf("failed to construct ephemeral private key from RFC fixture: %v", err)
	}
	salt := mustDecodeB64URL(t, rfc8291Salt)

	got, err := encryptAES128GCMWithKeys(ephemeral, salt, rfc8291UAPublic, rfc8291Auth, []byte(rfc8291Plaintext))
	if err != nil {
		t.Fatalf("encryptAES128GCMWithKeys returned error: %v", err)
	}
	if len(got) < 86 {
		t.Fatalf("expected at least an 86-byte header, got %d total bytes", len(got))
	}

	wantHeader := mustDecodeB64URL(t, "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8")
	if !bytes.Equal(got[:86], wantHeader) {
		t.Fatalf("RFC 8291 Appendix A header mismatch:\n got:  %s\n want: %s",
			base64.RawURLEncoding.EncodeToString(got[:86]),
			base64.RawURLEncoding.EncodeToString(wantHeader))
	}
}

func TestEncryptAES128GCMWithKeys_RejectsMalformedP256dh(t *testing.T) {
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("unexpected keygen error: %v", err)
	}
	salt := make([]byte, 16)

	shortP256dh := base64.RawURLEncoding.EncodeToString([]byte("too-short"))
	auth := base64.RawURLEncoding.EncodeToString(make([]byte, 16))

	_, err = encryptAES128GCMWithKeys(ephemeral, salt, shortP256dh, auth, []byte("hello"))
	if err == nil || !strings.Contains(err.Error(), "p256dh") {
		t.Fatalf("expected p256dh length error, got: %v", err)
	}
}

func TestEncryptAES128GCMWithKeys_RejectsMalformedAuth(t *testing.T) {
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("unexpected keygen error: %v", err)
	}
	receiver, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("unexpected keygen error: %v", err)
	}
	salt := make([]byte, 16)

	p256dh := base64.RawURLEncoding.EncodeToString(receiver.PublicKey().Bytes())
	shortAuth := base64.RawURLEncoding.EncodeToString([]byte("short"))

	_, err = encryptAES128GCMWithKeys(ephemeral, salt, p256dh, shortAuth, []byte("hello"))
	if err == nil || !strings.Contains(err.Error(), "auth") {
		t.Fatalf("expected auth length error, got: %v", err)
	}
}

func TestEncryptAES128GCMWithKeys_RejectsOversizedPlaintext(t *testing.T) {
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("unexpected keygen error: %v", err)
	}
	receiver, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("unexpected keygen error: %v", err)
	}
	salt := make([]byte, 16)
	p256dh := base64.RawURLEncoding.EncodeToString(receiver.PublicKey().Bytes())
	auth := base64.RawURLEncoding.EncodeToString(make([]byte, 16))

	oversized := bytes.Repeat([]byte("a"), MaxPlaintextSize+1)
	_, err = encryptAES128GCMWithKeys(ephemeral, salt, p256dh, auth, oversized)
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum plaintext size") {
		t.Fatalf("expected oversized plaintext error, got: %v", err)
	}
}

func TestEncryptAES128GCMWithKeys_AcceptsPlaintextAtBoundary(t *testing.T) {
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("unexpected keygen error: %v", err)
	}
	receiver, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("unexpected keygen error: %v", err)
	}
	salt := make([]byte, 16)
	p256dh := base64.RawURLEncoding.EncodeToString(receiver.PublicKey().Bytes())
	auth := base64.RawURLEncoding.EncodeToString(make([]byte, 16))

	atBoundary := bytes.Repeat([]byte("a"), MaxPlaintextSize)
	if _, err := encryptAES128GCMWithKeys(ephemeral, salt, p256dh, auth, atBoundary); err != nil {
		t.Fatalf("expected boundary-sized plaintext to be accepted, got error: %v", err)
	}
}

// decryptAES128GCMForTest is a test-only mirror of the RFC 8291 decryption
// steps, used solely for the supplementary round-trip sanity check below.
// It is deliberately not part of encrypt.go's production surface — this
// module has no receiver/decrypt role to play (see webpush.go's Send,
// which only ever encrypts).
func decryptAES128GCMForTest(t *testing.T, receiverPriv *ecdh.PrivateKey, authSecret []byte, body []byte) []byte {
	t.Helper()
	if len(body) < 86 {
		t.Fatalf("body too short to contain an aes128gcm header: %d bytes", len(body))
	}
	salt := body[0:16]
	idlen := int(body[20])
	keyID := body[21 : 21+idlen]
	ciphertext := body[21+idlen:]

	senderPub, err := ecdh.P256().NewPublicKey(keyID)
	if err != nil {
		t.Fatalf("failed to parse sender public key from header: %v", err)
	}
	ecdhSecret, err := receiverPriv.ECDH(senderPub)
	if err != nil {
		t.Fatalf("ECDH failed: %v", err)
	}

	uaPublic := receiverPriv.PublicKey().Bytes()
	keyInfo := "WebPush: info\x00" + string(uaPublic) + string(keyID)

	prkKey, err := hkdf.Extract(sha256.New, ecdhSecret, authSecret)
	if err != nil {
		t.Fatalf("hkdf.Extract (key combining) failed: %v", err)
	}
	ikm, err := hkdf.Expand(sha256.New, prkKey, keyInfo, 32)
	if err != nil {
		t.Fatalf("hkdf.Expand (IKM) failed: %v", err)
	}

	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		t.Fatalf("hkdf.Extract (content) failed: %v", err)
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		t.Fatalf("hkdf.Expand (CEK) failed: %v", err)
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		t.Fatalf("hkdf.Expand (nonce) failed: %v", err)
	}

	block, err := aes.NewCipher(cek)
	if err != nil {
		t.Fatalf("aes.NewCipher failed: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("cipher.NewGCM failed: %v", err)
	}
	padded, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatalf("AES-GCM decryption failed: %v", err)
	}
	if len(padded) == 0 || padded[len(padded)-1] != 0x02 {
		t.Fatalf("expected trailing 0x02 padding delimiter, got %x", padded)
	}
	return padded[:len(padded)-1]
}

// TestEncryptAES128GCM_RoundTrip is a supplementary sanity check only — not
// sufficient on its own (see the RFC 8291 fixed-vector test's doc comment
// above for why a bug symmetric in both directions could still round-trip).
func TestEncryptAES128GCM_RoundTrip(t *testing.T) {
	receiver, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("unexpected keygen error: %v", err)
	}
	authSecret := make([]byte, 16)
	if _, err := rand.Read(authSecret); err != nil {
		t.Fatalf("unexpected rand error: %v", err)
	}

	p256dh := base64.RawURLEncoding.EncodeToString(receiver.PublicKey().Bytes())
	auth := base64.RawURLEncoding.EncodeToString(authSecret)

	plaintext := []byte(`{"message":"hello world"}`)

	body, err := encryptAES128GCM(p256dh, auth, plaintext)
	if err != nil {
		t.Fatalf("encryptAES128GCM returned error: %v", err)
	}

	got := decryptAES128GCMForTest(t, receiver, authSecret, body)
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round-trip mismatch: got %q, want %q", got, plaintext)
	}
}
