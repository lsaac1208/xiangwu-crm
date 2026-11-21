package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

const encryptedFieldPrefix = "enc:v1:"

// FieldProtector encrypts sensitive scalar fields before persistence and
// decrypts them when loading from storage.
type FieldProtector interface {
	Protect(plaintext string) (string, error)
	Reveal(value string) (string, error)
}

type AESGCMFieldProtector struct {
	aead cipher.AEAD
}

func NewAESGCMFieldProtector(rawKey string) (*AESGCMFieldProtector, error) {
	key, err := parseAESKey(rawKey)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	return &AESGCMFieldProtector{aead: aead}, nil
}

func (p *AESGCMFieldProtector) Protect(plaintext string) (string, error) {
	// This boundary accepts domain plaintext, including text that happens to
	// start with the storage prefix. Only Reveal interprets storage markers.
	if plaintext == "" {
		return plaintext, nil
	}
	nonce := make([]byte, p.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := p.aead.Seal(nil, nonce, []byte(plaintext), nil)
	payload := append(nonce, ciphertext...)
	return encryptedFieldPrefix + base64.RawStdEncoding.EncodeToString(payload), nil
}

func (p *AESGCMFieldProtector) Reveal(value string) (string, error) {
	if value == "" || !strings.HasPrefix(value, encryptedFieldPrefix) {
		return value, nil
	}
	encoded := strings.TrimPrefix(value, encryptedFieldPrefix)
	payload, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode encrypted field: %w", err)
	}
	nonceSize := p.aead.NonceSize()
	if len(payload) <= nonceSize {
		return "", errors.New("encrypted field payload too short")
	}
	plaintext, err := p.aead.Open(nil, payload[:nonceSize], payload[nonceSize:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt field: %w", err)
	}
	return string(plaintext), nil
}

func IsEncryptedField(value string) bool {
	return strings.HasPrefix(value, encryptedFieldPrefix)
}

func parseAESKey(rawKey string) ([]byte, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return nil, errors.New("field encryption key is required")
	}
	if strings.HasPrefix(rawKey, "base64:") {
		rawKey = strings.TrimPrefix(rawKey, "base64:")
	}

	for _, decoder := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if decoded, err := decoder.DecodeString(rawKey); err == nil && len(decoded) == 32 {
			return decoded, nil
		}
	}

	if len([]byte(rawKey)) == 32 {
		return []byte(rawKey), nil
	}
	return nil, errors.New("field encryption key must be 32 bytes or base64-encoded 32 bytes")
}
