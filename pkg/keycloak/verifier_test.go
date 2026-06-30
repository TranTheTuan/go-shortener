package keycloak

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

const testKID = "test-key-1"

// jwksServer spins up an httptest server exposing a JWKS for the given public
// key, and returns a signer for minting matching RS256 tokens.
func jwksServer(t *testing.T) (url string, sign func(claims map[string]any) string) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa key: %v", err)
	}

	jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: priv.Public(), KeyID: testKID, Algorithm: "RS256", Use: "sig",
	}}}
	body, _ := json.Marshal(jwks)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: priv, KeyID: testKID}},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}

	return srv.URL, func(claims map[string]any) string {
		payload, _ := json.Marshal(claims)
		jws, err := signer.Sign(payload)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		tok, _ := jws.CompactSerialize()
		return tok
	}
}

const issuer = "https://auth.cd.me/realms/test"

func baseClaims() map[string]any {
	now := time.Now()
	return map[string]any{
		"iss":                issuer,
		"sub":                "kc-uuid-1",
		"email":              "alice@example.com",
		"preferred_username": "alice",
		"iat":                now.Add(-time.Minute).Unix(),
		"exp":                now.Add(time.Hour).Unix(),
	}
}

func TestVerifier_ValidToken(t *testing.T) {
	jwksURL, sign := jwksServer(t)
	v := NewVerifier(context.Background(), issuer, jwksURL, "") // skip aud

	id, err := v.Verify(context.Background(), sign(baseClaims()))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if id.Sub != "kc-uuid-1" || id.Email != "alice@example.com" || id.Username != "alice" {
		t.Errorf("identity = %+v, want sub/email/preferred_username extracted", id)
	}
}

func TestVerifier_Rejects(t *testing.T) {
	jwksURL, sign := jwksServer(t)
	v := NewVerifier(context.Background(), issuer, jwksURL, "")

	t.Run("wrong issuer", func(t *testing.T) {
		c := baseClaims()
		c["iss"] = "https://evil.example/realms/test"
		if _, err := v.Verify(context.Background(), sign(c)); err == nil {
			t.Error("expected error for wrong issuer")
		}
	})

	t.Run("expired", func(t *testing.T) {
		c := baseClaims()
		c["exp"] = time.Now().Add(-time.Hour).Unix()
		if _, err := v.Verify(context.Background(), sign(c)); err == nil {
			t.Error("expected error for expired token")
		}
	})

	t.Run("garbage token", func(t *testing.T) {
		if _, err := v.Verify(context.Background(), "not-a-jwt"); err == nil {
			t.Error("expected error for malformed token")
		}
	})
}

func TestVerifier_ClientBinding(t *testing.T) {
	jwksURL, sign := jwksServer(t)
	v := NewVerifier(context.Background(), issuer, jwksURL, "go-shortener") // require client

	t.Run("matching aud passes", func(t *testing.T) {
		c := baseClaims()
		c["aud"] = "go-shortener"
		if _, err := v.Verify(context.Background(), sign(c)); err != nil {
			t.Errorf("expected ok for matching aud, got %v", err)
		}
	})

	t.Run("matching azp passes (Keycloak default aud=account)", func(t *testing.T) {
		c := baseClaims()
		c["aud"] = "account" // Keycloak's default access-token audience
		c["azp"] = "go-shortener"
		if _, err := v.Verify(context.Background(), sign(c)); err != nil {
			t.Errorf("expected ok when azp matches the client, got %v", err)
		}
	})

	t.Run("neither aud nor azp matches → rejected", func(t *testing.T) {
		c := baseClaims()
		c["aud"] = "account"
		c["azp"] = "some-other-client"
		if _, err := v.Verify(context.Background(), sign(c)); err == nil {
			t.Error("expected error when neither aud nor azp matches the client")
		}
	})
}
