// Package token issues and verifies signed JWT access tokens (HS256). It keeps
// JWT concerns out of the service layer behind a small Issuer type.
package token

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidToken is returned for any token that fails parsing, signature, or
// expiry validation. Callers map it to a generic 401 without leaking detail.
var ErrInvalidToken = errors.New("token: invalid or expired")

// Claims is the access-token payload. UserID is mirrored into the standard
// Subject claim so the token is self-describing.
type Claims struct {
	UserID int64 `json:"uid"`
	jwt.RegisteredClaims
}

// Issuer signs and parses access tokens with a shared secret.
type Issuer struct {
	secret    []byte
	accessTTL time.Duration
	now       func() time.Time
}

// NewIssuer builds an Issuer with the given HMAC secret and access-token TTL.
func NewIssuer(secret string, accessTTL time.Duration) *Issuer {
	return &Issuer{
		secret:    []byte(secret),
		accessTTL: accessTTL,
		now:       time.Now,
	}
}

// AccessTTL exposes the configured access-token lifetime.
func (i *Issuer) AccessTTL() time.Duration { return i.accessTTL }

// Issue returns a signed access token for the given user ID.
func (i *Issuer) Issue(userID int64) (string, error) {
	now := i.now().UTC()
	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(userID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(i.accessTTL)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(i.secret)
	if err != nil {
		return "", fmt.Errorf("token: sign: %w", err)
	}
	return signed, nil
}

// Parse validates the token's signature and expiry and returns its claims.
// It pins the signing method to HMAC to prevent algorithm-confusion attacks.
// Any failure is reported as ErrInvalidToken.
func (i *Issuer) Parse(tokenStr string) (*Claims, error) {
	var claims Claims
	_, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return i.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		return nil, ErrInvalidToken
	}
	return &claims, nil
}
