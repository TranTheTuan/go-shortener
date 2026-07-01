# Phase 03 — Docs (README + Swagger)

**Context:** [plan.md](plan.md) · [spec](../reports/spec-260701-1613-user-links-list.md)

## Overview
- **Priority:** Medium (DoD)
- **Status:** pending
- Reflect the new endpoint + UI in docs.

## Related Code Files
- **Modify:** `README.md`, Swagger (`make swag`)

## Implementation Steps

1. **README API table** — add:
   ```
   | GET | /api/links | Keycloak JWT | List the caller's links (paginated: ?limit=&offset=, with click counts) |
   ```
   In the Frontend section, mention the "My links" list (paginated, with click counts).

2. **Swagger** — `make swag` to regenerate from the `List` handler annotations; confirm `GET /api/links` appears with `limit`/`offset` params + `BearerAuth`.

3. **Gate:** `make build` && `make test` (all green) && `gofmt`/`make lint`.

## Todo
- [ ] README API table + Frontend note
- [ ] `make swag` regenerate
- [ ] `make build` + `make test` green

## Success Criteria
- Docs describe `GET /api/links` (pagination + click counts) and the My-links UI; Swagger regenerated; suite green.

## Next
Plan complete → mark phases done; optional `/plan archive`.
