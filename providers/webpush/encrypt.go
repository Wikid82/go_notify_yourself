package webpush

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
)

// MaxPlaintextSize is the largest plaintext payload encryptAES128GCM will
// accept, in bytes. RFC 8188 §2 single-record framing adds a fixed 86-byte
// record header (salt(16) + rs(4) + idlen(1) + keyid(65)) plus a 1-byte
// delimiter and a 16-byte AES-GCM tag around the plaintext (103 bytes of
// fixed overhead total), and real push services independently cap the
// resulting request body at roughly 4096 bytes (FCM and Mozilla autopush
// both document limits in this neighborhood). MaxPlaintextSize is set well
// inside that ceiling (86 + 3800 + 17 = 3903 bytes total, vs. a ~4096-byte
// external cap) rather than exactly at the boundary, so a plaintext this
// module accepts is not immediately at risk of a push-service-side
// rejection this module can't see coming.
const MaxPlaintextSize = 3800

// recordSize is the RFC 8188 §2 "rs" field value this module always emits.
// Every payload webpush sends is a single, final record, so any value at
// least as large as the total framed record works; 4096 matches the value
// RFC 8291's own Appendix A example uses.
const recordSize = 4096

// aes128gcmHeaderSize is the fixed RFC 8188 §2 single-record header size
// for a 65-byte (uncompressed P-256) keyid: salt(16) + rs(4) + idlen(1) +
// keyid(65).
const aes128gcmHeaderSize = 16 + 4 + 1 + 65

// paddingDelimiter is the RFC 8188 §2 delimiter octet appended to the
// plaintext of a single, final record.
const paddingDelimiter = 0x02

// encryptAES128GCM implements RFC 8291 Web Push message encryption. Given
// the subscriber's base64url (no padding) encoded p256dh public key and
// auth secret (from PushSubscription.getKey), and the plaintext
// application payload, it returns the aes128gcm content-coded ciphertext
// (RFC 8188 §2 single-record framing: salt(16) || rs(4) || idlen(1) ||
// keyid(65, the ephemeral sender public key, uncompressed) ||
// AEAD-ciphertext) ready to send as the request body. Generates a fresh
// ephemeral P-256 keypair and a fresh random 16-byte salt per call — see
// encryptAES128GCMWithKeys for the deterministic variant tests use.
func encryptAES128GCM(p256dhB64, authB64 string, plaintext []byte) ([]byte, error) {
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("webpush: generate ephemeral keypair: %w", err)
	}

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("webpush: generate salt: %w", err)
	}

	return encryptAES128GCMWithKeys(ephemeral, salt, p256dhB64, authB64, plaintext)
}

// encryptAES128GCMWithKeys is encryptAES128GCM with the ephemeral sender
// keypair and salt injected rather than randomly generated — the
// production encryptAES128GCM is a thin wrapper generating both randomly
// and delegating here. Exists so tests (in particular the RFC 8291
// Appendix A fixed-vector test, encrypt_test.go) can force the exact
// keys/salt the RFC's published example uses and assert exact-byte
// output — a capability a purely-random production path can't otherwise
// be tested against without an external reference implementation, which
// this dependency-free module cannot depend on.
func encryptAES128GCMWithKeys(ephemeral *ecdh.PrivateKey, salt []byte, p256dhB64, authB64 string, plaintext []byte) ([]byte, error) {
	if len(plaintext) > MaxPlaintextSize {
		return nil, fmt.Errorf("webpush: payload of %d bytes exceeds maximum plaintext size of %d bytes", len(plaintext), MaxPlaintextSize)
	}

	uaPublicRaw, err := base64.RawURLEncoding.DecodeString(p256dhB64)
	if err != nil {
		return nil, fmt.Errorf("webpush: p256dh: invalid base64url encoding: %w", err)
	}
	if len(uaPublicRaw) != 65 {
		return nil, fmt.Errorf("webpush: p256dh: expected 65-byte uncompressed P-256 point, got %d bytes", len(uaPublicRaw))
	}

	authSecret, err := base64.RawURLEncoding.DecodeString(authB64)
	if err != nil {
		return nil, fmt.Errorf("webpush: auth: invalid base64url encoding: %w", err)
	}
	if len(authSecret) != 16 {
		return nil, fmt.Errorf("webpush: auth: expected 16-byte secret, got %d bytes", len(authSecret))
	}

	subscriberPub, err := ecdh.P256().NewPublicKey(uaPublicRaw)
	if err != nil {
		return nil, fmt.Errorf("webpush: p256dh: invalid P-256 point: %w", err)
	}

	ecdhSecret, err := ephemeral.ECDH(subscriberPub)
	if err != nil {
		return nil, fmt.Errorf("webpush: ECDH key agreement failed: %w", err)
	}

	asPublicRaw := ephemeral.PublicKey().Bytes()

	// RFC 8291 §3.4 step 1: derive the key-combining IKM from the ECDH
	// shared secret and the subscriber's auth secret.
	keyInfo := "WebPush: info\x00" + string(uaPublicRaw) + string(asPublicRaw)
	prkKey, err := hkdf.Extract(sha256.New, ecdhSecret, authSecret)
	if err != nil {
		return nil, fmt.Errorf("webpush: HKDF-Extract (key combining): %w", err)
	}
	ikm, err := hkdf.Expand(sha256.New, prkKey, keyInfo, 32)
	if err != nil {
		return nil, fmt.Errorf("webpush: HKDF-Expand (IKM): %w", err)
	}

	// RFC 8291 §3.4 step 2: derive the content-encryption key and nonce
	// from the IKM and the (random or injected) 16-byte salt.
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, fmt.Errorf("webpush: HKDF-Extract (content encryption): %w", err)
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, fmt.Errorf("webpush: HKDF-Expand (CEK): %w", err)
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, fmt.Errorf("webpush: HKDF-Expand (nonce): %w", err)
	}

	// RFC 8188 §2: a single, final record gets a 0x02 padding delimiter.
	padded := make([]byte, 0, len(plaintext)+1)
	padded = append(padded, plaintext...)
	padded = append(padded, paddingDelimiter)

	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, fmt.Errorf("webpush: construct AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("webpush: construct AES-GCM AEAD: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, padded, nil)

	header := make([]byte, aes128gcmHeaderSize)
	copy(header[0:16], salt)
	binary.BigEndian.PutUint32(header[16:20], recordSize)
	header[20] = byte(len(asPublicRaw))
	copy(header[21:aes128gcmHeaderSize], asPublicRaw)

	return append(header, ciphertext...), nil
}
