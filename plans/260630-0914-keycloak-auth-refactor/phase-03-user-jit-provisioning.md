# Phase 03 — User Repo/Service JIT Provisioning

**Context:** [plan.md](plan.md) · [spec](../reports/spec-260630-0914-keycloak-auth-refactor.md)

## Overview
- **Priority:** High
- **Status:** pending
- Map Keycloak identity → local int64 user (get-or-create) so ownership/quota are unchanged.

## Related Code Files
- **Modify:** `internal/repository/user_repository.go`, `internal/service/user_service.go`

## Implementation Steps

1. **`user_repository.go`** — `User` struct: add `KeycloakSub *string `gorm:"size:36;uniqueIndex" json:"-"``;
   **remove** `PasswordHash` field. Add to `UserRepository`:
   ```go
   GetByKeycloakSub(ctx context.Context, sub string) (*User, error) // ErrNotFound if absent
   ```
   GORM impl: `Where("keycloak_sub = ?", sub).First(&u)`, map `ErrRecordNotFound`→`ErrNotFound`.

2. **`user_service.go`** — add to `UserService`:
   ```go
   SyncFromKeycloak(ctx context.Context, id keycloak.Identity) (*repository.User, error)
   ```
   (import `pkg/keycloak` for `Identity`, OR define a small local `SyncInput{Sub,Email,Username}` to avoid the dep — prefer `SyncInput` to keep service free of the keycloak pkg.)
   Logic:
   ```go
   u, err := s.repo.GetByKeycloakSub(ctx, in.Sub)
   if err == nil {
       // refresh email/username if Keycloak changed them
       if changed(u, in) { u.Email, u.Username = in.Email, in.Username; s.repo.Update(ctx, u) }
       return u, nil
   }
   if !errors.Is(err, repository.ErrNotFound) { return nil, apperror.Internal(err) }
   sub := in.Sub
   created, err := s.repo.Create(ctx, &repository.User{
       KeycloakSub: &sub, Email: in.Email, Username: in.Username, CreatedAt: time.Now().UTC(),
   })
   if errors.Is(err, repository.ErrConflict) { // race: created concurrently → re-fetch
       return s.repo.GetByKeycloakSub(ctx, in.Sub)
   }
   if err != nil { return nil, apperror.Internal(err) }
   return created, nil
   ```
   Add `Update(ctx, *User) error` to `UserRepository` if not present (or `Save`). Keep `GetUser`/`ListUsers`.
   Note: `Name` is `*string` (optional) — leave nil or set from a `name` claim later (out of scope).

3. `go build ./internal/repository/ ./internal/service/` (auth_service.go is removed in Phase 04; if it still references PasswordHash, expect a break resolved there — or stub now).

## Key Insight
The unique index on `keycloak_sub` makes get-or-create race-safe: a concurrent create loses on the unique constraint (`ErrConflict`) → re-fetch returns the winner's row.

## Todo
- [ ] `User`: +`KeycloakSub`, −`PasswordHash`
- [ ] `GetByKeycloakSub` + `Update`/`Save`
- [ ] `SyncFromKeycloak` (get-or-create + race handling + claim refresh)
- [ ] build (repo+service) passes

## Success Criteria
- Compiles; unit-testable get-or-create (tests in Phase 05).

## Next
Phase 04 middleware calls `SyncFromKeycloak` and stamps the local id.
