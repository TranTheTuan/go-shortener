// Package keycloak validates Keycloak-issued OIDC access tokens. It fetches the
// realm's public keys from an in-cluster JWKS endpoint while validating the
// public issuer claim, keeping verification local (no per-request network call)
// and decoupling the key-fetch URL from the token issuer (split-horizon).
package keycloak

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Identity is the subset of token claims the application needs.
type Identity struct {
	Sub        string // Keycloak user UUID (the `sub` claim)
	Email      string
	Username   string // the `preferred_username` claim
	GivenName  string // the `given_name` claim
	FamilyName string // the `family_name` claim
}

// TokenVerifier validates a raw bearer token and returns the caller identity.
// It is an interface so the auth middleware can be unit-tested without Keycloak.
type TokenVerifier interface {
	Verify(ctx context.Context, rawToken string) (*Identity, error)
}

// oidcVerifier is the go-oidc backed TokenVerifier.
type oidcVerifier struct {
	verifier *oidc.IDTokenVerifier
	clientID string // expected client; matched against aud OR azp ("" = skip)
}

// NewVerifier builds a TokenVerifier that fetches JWKS from jwksURL (in-cluster)
// and validates the token's issuer against issuer (the public realm URL).
//
// ctx must be the long-lived application context: oidc.NewRemoteKeySet uses it
// for lazy fetching and background key rotation. The key set is fetched on the
// first Verify (not here), so construction never blocks on Keycloak.
func NewVerifier(ctx context.Context, issuer, jwksURL, clientID string) TokenVerifier {
	keySet := oidc.NewRemoteKeySet(ctx, jwksURL)
	// SkipClientIDCheck: signature/iss/exp are still enforced; client binding is
	// done manually (aud OR azp) in Verify.
	cfg := &oidc.Config{
		ClientID:          clientID,
		SkipClientIDCheck: clientID == "",
	}
	return &oidcVerifier{verifier: oidc.NewVerifier(issuer, keySet, cfg), clientID: clientID}
}

// Verify validates signature, issuer, and expiry; binds the token to the
// expected client (aud or azp); then extracts the identity claims.
func (o *oidcVerifier) Verify(ctx context.Context, rawToken string) (*Identity, error) {
	tok, err := o.verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, err
	}

	var claims struct {
		Sub        string `json:"sub"`
		Email      string `json:"email"`
		Username   string `json:"preferred_username"`
		Azp        string `json:"azp"`
		GivenName  string `json:"given_name"`
		FamilyName string `json:"family_name"`
	}
	if err := tok.Claims(&claims); err != nil {
		return nil, err
	}

	if o.clientID != "" && !clientMatches(o.clientID, tok.Audience, claims.Azp) {
		return nil, fmt.Errorf("oidc: token not issued for client %q (aud=%v azp=%q)", o.clientID, tok.Audience, claims.Azp)
	}

	return &Identity{Sub: claims.Sub, Email: claims.Email, Username: claims.Username, GivenName: claims.GivenName, FamilyName: claims.FamilyName}, nil
}

// clientMatches reports whether the expected client appears in the token's
// audience list or is the authorized party (azp).
func clientMatches(client string, audience []string, azp string) bool {
	if azp == client {
		return true
	}
	for _, a := range audience {
		if a == client {
			return true
		}
	}
	return false
}
