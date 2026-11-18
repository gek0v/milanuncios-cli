package milanuncios

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ParseJWTExpiration extracts the expiration timestamp from a JWT token's claims.
// Returns (expiresAt, isExpired, error). If the token is not a JWT or has no exp claim,
// returns (nil, false, nil).
func ParseJWTExpiration(token string) (*time.Time, bool, error) {
	trimmed := strings.TrimSpace(token)
	parts := strings.Split(trimmed, ".")
	if len(parts) != 3 {
		return nil, false, nil
	}

	payloadSeg := parts[1]
	// Decode base64url payload segment (first try RawURLEncoding without padding, then URLEncoding with padding)
	data, err := base64.RawURLEncoding.DecodeString(payloadSeg)
	if err != nil {
		data, err = base64.URLEncoding.DecodeString(payloadSeg)
		if err != nil {
			return nil, false, fmt.Errorf("decoding jwt payload: %w", err)
		}
	}

	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(data, &claims); err != nil {
		return nil, false, fmt.Errorf("parsing jwt claims: %w", err)
	}

	if claims.Exp == 0 {
		return nil, false, nil
	}

	expTime := time.Unix(claims.Exp, 0).UTC()
	isExpired := time.Now().UTC().After(expTime)
	return &expTime, isExpired, nil
}
