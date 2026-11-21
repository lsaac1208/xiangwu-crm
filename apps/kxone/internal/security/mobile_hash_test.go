package security

import (
	"strings"
	"testing"
)

func TestHMACMobileHasherNormalizesAndHashes(t *testing.T) {
	hasher, err := NewHMACMobileHasher("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("new hasher: %v", err)
	}

	first := hasher.HashMobile("139 1234-5678")
	second := hasher.HashSearchKeyword("+86 13912345678")
	if first == "" || second == "" || first != second {
		t.Fatalf("hash mismatch: first=%q second=%q", first, second)
	}
	if strings.Contains(first, "13912345678") || !strings.HasPrefix(first, mobileHashPrefix) {
		t.Fatalf("unexpected hash format: %q", first)
	}
}

func TestHMACMobileHasherIgnoresShortSearchKeyword(t *testing.T) {
	hasher, err := NewHMACMobileHasher("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("new hasher: %v", err)
	}
	if got := hasher.HashSearchKeyword("5678"); got != "" {
		t.Fatalf("short keyword hash = %q, want empty", got)
	}
	if got := hasher.HashSearchKeyword("12345678"); got != "" {
		t.Fatalf("partial mobile hash = %q, want empty", got)
	}
}
