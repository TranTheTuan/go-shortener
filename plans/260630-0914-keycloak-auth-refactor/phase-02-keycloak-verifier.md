# Phase 02 — pkg/keycloak Verifier

**Context:** [plan.md](plan.md) · [spec](../reports/spec-260630-0914-keycloak-auth-refactor.md)

## Overview
- **Priority:** High
- **Status:** pending
- go-oidc wrapper that validates Keycloak access tokens (internal JWKS, public issuer), behind a testable interface.

## Related Code Files
- **Create:** `pkg/keycloak/verifier.go` (+ `verifier_test.go`)

## Implementation Steps

1. **`pkg/keycloak/verifier.go`**:
   ```go
   type Identity struct{ Sub, Email, Username string }

   // TokenVerifier validates a raw bearer token and returns the caller identity.
   type TokenVerifier interface {
       Verify(ctx context.Context, rawToken string) (*Identity, error)
   }

   type oidcVerifier struct{ v *oidc.IDTokenVerifier }

   // NewVerifier builds a verifier that fetches JWKS from jwksURL (in-cluster)
   // but validates iss == issuer (public). clientID "" → SkipClientIDCheck.
   func NewVerifier(ctx context.Context, issuer, jwksURL, clientID string) TokenVerifier {
       keySet := oidc.NewRemoteKeySet(ctx, jwksURL) // lazy: no fetch until first Verify
       cfg := &oidc.Config{ClientID: clientID, SkipClientIDCheck: clientID == ""}
       return &oidcVerifier{v: oidc.NewVerifier(issuer, keySet, cfg)}
   }

   func (o *oidcVerifier) Verify(ctx context.Context, raw string) (*Identity, error) {
       tok, err := o.v.Verify(ctx, raw)
       if err != nil { return nil, err }
       var c struct {
           Sub      string `json:"sub"`
           Email    string `json:"email"`
           Username string `json:"preferred_username"`
       }
       if err := tok.Claims(&c); err != nil { return nil, err }
       return &Identity{Sub: c.Sub, Email: c.Email, Username: c.Username}, nil
   }
   ```
   - `NewVerifier` takes a `ctx` whose lifetime spans the app (the RemoteKeySet uses it for background refresh) — pass the long-lived app context from main, NOT a request context.

2. `go build ./pkg/keycloak/`.

## Key Insights
- `oidc.NewRemoteKeySet(ctx, jwksURL)` is lazy + auto-refreshing + caches keys → no startup network call, no per-request fetch.
- `oidc.NewVerifier(issuer, …)` decouples the expected `iss` from the JWKS URL — solves the public-iss / internal-JWKS split.
- Keep the `TokenVerifier` interface so the middleware (Phase 04) is unit-testable without Keycloak.

## Todo
- [ ] `Identity` + `TokenVerifier` interface
- [ ] `oidcVerifier` (RemoteKeySet + NewVerifier + claim extraction)
- [ ] `verifier_test.go` (httptest JWKS + RSA-signed token: ok / wrong-iss / expired / bad-sig)
- [ ] build passes

## Success Criteria
- Compiles; verifier returns Identity for a valid token and errors for invalid iss/exp/signature (tests in Phase 05 may host the JWKS fixture, or include a focused test here).

## Security
HMAC/alg confusion is not possible (go-oidc pins RS/ES algorithms from JWKS). `iss`/`aud`/`exp` enforced.

## Next
Phase 04 middleware consumes `TokenVerifier`.
