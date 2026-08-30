package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Claims represents user token claims and metadata.
type Claims struct {
	UserID    string   `json:"sub"`
	Username  string   `json:"username"`
	Roles     []string `json:"roles"`
	ExpiresAt int64    `json:"exp"`
	IssuedAt  int64    `json:"iat"`
}

// TokenManager handles issuing and verifying JSON Web Tokens.
type TokenManager struct {
	secretKey []byte
	ttl       time.Duration
}

// NewTokenManager initializes a TokenManager with a secret key.
func NewTokenManager(secret string, ttl time.Duration) *TokenManager {
	return &TokenManager{
		secretKey: []byte(secret),
		ttl:       ttl,
	}
}

// GenerateToken creates a signed JWT string for the given user.
func (m *TokenManager) GenerateToken(userID, username string, roles []string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:    userID,
		Username:  username,
		Roles:     roles,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(m.ttl).Unix(),
	}

	headerJSON := `{"alg":"HS256","typ":"JWT"}`
	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(headerJSON))

	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	payload := headerB64 + "." + claimsB64
	sig := m.sign(payload)

	return payload + "." + sig, nil
}

// ValidateToken verifies token signature and expiration, returning parsed claims.
func (m *TokenManager) ValidateToken(tokenStr string) (*Claims, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed jwt token")
	}

	payload := parts[0] + "." + parts[1]
	expectedSig := m.sign(payload)
	if parts[2] != expectedSig {
		return nil, errors.New("invalid jwt token signature")
	}

	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("failed decoding token payload")
	}

	var claims Claims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return nil, errors.New("failed unmarshaling claims")
	}

	if time.Now().Unix() > claims.ExpiresAt {
		return nil, errors.New("jwt token has expired")
	}

	return &claims, nil
}

func (m *TokenManager) sign(payload string) string {
	mac := hmac.New(sha256.New, m.secretKey)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
