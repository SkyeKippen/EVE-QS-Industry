package db

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestTokenEncryptionRoundTrip(t *testing.T) {
	t.Setenv("TOKEN_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))

	sealed, err := encryptToken("refresh-token-value")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sealed), "refresh-token-value") {
		t.Fatal("sealed token contains the plaintext")
	}

	plain, err := decryptToken(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "refresh-token-value" {
		t.Fatalf("got %q after round trip", plain)
	}
}

func TestTokenDecryptionFailsWithAnotherKey(t *testing.T) {
	t.Setenv("TOKEN_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("a", 32))))
	sealed, err := encryptToken("secret")
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("TOKEN_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("b", 32))))
	if _, err := decryptToken(sealed); err == nil {
		t.Fatal("decrypting with a different key should fail")
	}
}

func TestTokenEncryptionRejectsBadKeys(t *testing.T) {
	for _, key := range []string{"", "not base64!", base64.StdEncoding.EncodeToString([]byte("too short"))} {
		t.Setenv("TOKEN_ENCRYPTION_KEY", key)
		if _, err := encryptToken("x"); err == nil {
			t.Fatalf("key %q should be rejected", key)
		}
	}
}
