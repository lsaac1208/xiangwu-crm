package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
)

const mobileHashPrefix = "hmac:v1:"

type MobileHasher interface {
	HashMobile(mobile string) string
	HashSearchKeyword(keyword string) string
}

type HMACMobileHasher struct {
	key []byte
}

func NewHMACMobileHasher(rawKey string) (*HMACMobileHasher, error) {
	key, err := parseAESKey(rawKey)
	if err != nil {
		return nil, err
	}
	return &HMACMobileHasher{key: key}, nil
}

func (h *HMACMobileHasher) HashMobile(mobile string) string {
	normalized := NormalizeMobile(mobile)
	if normalized == "" {
		return ""
	}
	return h.hashNormalized(normalized)
}

func (h *HMACMobileHasher) HashSearchKeyword(keyword string) string {
	normalized := NormalizeMobile(keyword)
	if !LooksLikeFullMobile(normalized) {
		return ""
	}
	return h.hashNormalized(normalized)
}

func (h *HMACMobileHasher) hashNormalized(normalized string) string {
	mac := hmac.New(sha256.New, h.key)
	mac.Write([]byte("kxone:mobile:v1:"))
	mac.Write([]byte(normalized))
	return mobileHashPrefix + hex.EncodeToString(mac.Sum(nil))
}

func NormalizeMobile(value string) string {
	var builder strings.Builder
	for _, r := range value {
		if unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	normalized := builder.String()
	if len(normalized) == 13 && strings.HasPrefix(normalized, "86") {
		return normalized[2:]
	}
	return normalized
}

func LooksLikeFullMobile(normalized string) bool {
	return len(normalized) == 11 && strings.HasPrefix(normalized, "1")
}
