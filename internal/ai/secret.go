// Package ai implements LLM pre-translation: an OpenAI-compatible client,
// glossary-constrained prompts, result verification and cost estimates.
package ai

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// Box encrypts secrets such as API keys at rest (AES-256-GCM)
type Box struct {
	aead cipher.AEAD
}

// NewBox derives an encryption key from a secret string
func NewBox(secret string) *Box {
	key := sha256.Sum256([]byte("pelokalan-ai-key:" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		panic(err) // a 32-byte key never fails
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return &Box{aead: aead}
}

// Seal encrypts plaintext
func (b *Box) Seal(plaintext string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := b.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

// Open decrypts a value produced by Seal
func (b *Box) Open(sealed string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < b.aead.NonceSize() {
		return "", errors.New("invalid secret")
	}
	n := b.aead.NonceSize()
	plain, err := b.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", errors.New("cannot decrypt secret (was the encryption key changed?)")
	}
	return string(plain), nil
}

// KeyHint returns a safe representation of an API key, e.g. "sk-…a1b2"
func KeyHint(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 8 {
		return "…"
	}
	prefix := ""
	if i := strings.Index(key, "-"); i > 0 && i < 8 {
		prefix = key[:i+1]
	}
	return prefix + "…" + key[len(key)-4:]
}
