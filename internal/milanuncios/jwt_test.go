package milanuncios_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/gek0v/milanuncios-cli/internal/milanuncios"
)

func createTestJWT(exp int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims := map[string]any{"sub": "12345", "name": "Test User"}
	if exp != 0 {
		claims["exp"] = exp
	}
	claimsBytes, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(claimsBytes)
	signature := base64.RawURLEncoding.EncodeToString([]byte("signature-bytes"))
	return fmt.Sprintf("%s.%s.%s", header, payload, signature)
}

func TestParseJWTExpiration(t *testing.T) {
	// 1. Future expiration
	futureExp := time.Now().Add(2 * time.Hour).Unix()
	validToken := createTestJWT(futureExp)
	expTime, isExpired, err := milanuncios.ParseJWTExpiration(validToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if expTime == nil {
		t.Fatal("expected non-nil expTime")
	}
	if isExpired {
		t.Fatalf("expected isExpired to be false for future exp %v", expTime)
	}
	if expTime.Unix() != futureExp {
		t.Fatalf("expected exp %d, got %d", futureExp, expTime.Unix())
	}

	// 2. Past expiration (expired)
	pastExp := time.Now().Add(-2 * time.Hour).Unix()
	expiredToken := createTestJWT(pastExp)
	expTime, isExpired, err = milanuncios.ParseJWTExpiration(expiredToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if expTime == nil {
		t.Fatal("expected non-nil expTime")
	}
	if !isExpired {
		t.Fatalf("expected isExpired to be true for past exp %v", expTime)
	}

	// 3. No exp claim
	noExpToken := createTestJWT(0)
	expTime, isExpired, err = milanuncios.ParseJWTExpiration(noExpToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if expTime != nil || isExpired {
		t.Fatalf("expected nil expTime and false isExpired, got %v, %v", expTime, isExpired)
	}

	// 4. Non-JWT legacy token string
	legacyToken := "some-plain-hex-or-uuid-token-string"
	expTime, isExpired, err = milanuncios.ParseJWTExpiration(legacyToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if expTime != nil || isExpired {
		t.Fatalf("expected nil expTime and false isExpired, got %v, %v", expTime, isExpired)
	}

	// 5. Malformed base64 JWT payload
	malformedToken := "abc.invalid!base64!.xyz"
	_, _, err = milanuncios.ParseJWTExpiration(malformedToken)
	if err == nil {
		t.Fatal("expected error for malformed base64 payload, got nil")
	}
}
