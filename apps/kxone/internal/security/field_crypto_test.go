package security

import "testing"

func TestAESGCMFieldProtectorProtectsAndReveals(t *testing.T) {
	protector, err := NewAESGCMFieldProtector("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("new protector: %v", err)
	}

	encrypted, err := protector.Protect("13912345678")
	if err != nil {
		t.Fatalf("protect: %v", err)
	}
	if encrypted == "13912345678" || !IsEncryptedField(encrypted) {
		t.Fatalf("encrypted field = %q", encrypted)
	}

	plaintext, err := protector.Reveal(encrypted)
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	if plaintext != "13912345678" {
		t.Fatalf("plaintext = %q", plaintext)
	}
}

func TestAESGCMFieldProtectorRevealsLegacyPlaintext(t *testing.T) {
	protector, err := NewAESGCMFieldProtector("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("new protector: %v", err)
	}

	plaintext, err := protector.Reveal("13912345678")
	if err != nil {
		t.Fatalf("reveal legacy plaintext: %v", err)
	}
	if plaintext != "13912345678" {
		t.Fatalf("plaintext = %q", plaintext)
	}
}

func TestFieldProtectionTreatsCiphertextPrefixAsUserText(t *testing.T) {
	p, err := NewAESGCMFieldProtector("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"enc:v1:hello", "enc:v1:", "enc:v1:YWJj"} {
		stored, err := p.Protect(input)
		if err != nil {
			t.Fatal(err)
		}
		output, err := p.Reveal(stored)
		if err != nil || output != input || stored == input {
			t.Fatalf("user text did not round trip: err=%v", err)
		}
	}
}
