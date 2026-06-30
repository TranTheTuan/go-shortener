# Design Spec: Minimal Vanilla Frontend (served by the Go binary)

- **Date:** 2026-06-30
- **Status:** Pending user review
- **Scope:** A tiny vanilla HTML/CSS/JS single-page frontend that authenticates via Keycloak (keycloak-js, PKCE) and lets a signed-in user create short links. Served by the Go binary via `go:embed` (co-hosted, same origin, no API CORS).

## Problem Statement

The backend is a Keycloak-protected JSON API with no UI. Need a minimal browser frontend so a user can sign in (Keycloak) and create/copy short links. Keep it dead simple: no build step, no SPA framework, one deploy artifact.

## Decisions (locked)

| # | Decision | Choice |
|---|----------|--------|
| 1 | Stack | **Vanilla HTML+CSS+JS + keycloak-js** (no build pipeline) |
| 2 | Hosting | **Co-hosted via `go:embed`** — the Go binary serves the page + assets at the same origin as the API (no API CORS; one deploy) |
| 3 | Keycloak client | **Reuse the existing `go-shortener` public client** (its tokens carry `azp=go-shortener`, which the backend already accepts). Add the Go server origin to its redirect URIs / web origins |
| 4 | Scope | MVP: sign in/out, show profile (`/auth/me`), create link (`POST /api/links`) + copy, optional stats-by-code lookup. **No "my links" list** |

## Architecture

```
Browser ──/, /static/*, /app-config.json──►  Go server (embed.FS)         ← serves the SPA
   │                                              │
   │  keycloak-js: Authorization Code + PKCE      │  validates Bearer (existing Keycloak mw)
   ▼                                              ▼
auth.cd.me (Keycloak)  ──token──►  Browser ──Bearer──►  /auth/me, /api/links (same origin)
```

### Serving (`go:embed`)
- `web/` dir embedded into the binary: `index.html`, `static/app.js`, `static/styles.css`, `static/keycloak.min.js` (vendored), `embed.go` (`//go:embed index.html static` → `var Files embed.FS`).
- Routes (registered as exact/prefix so they win over the `/:code` redirect catch-all):
  - `GET /` → `index.html` (also receives the OIDC callback query; keycloak-js parses it).
  - `GET /static/*` → embedded assets via `e.StaticFS`.
  - `GET /app-config.json` → `{authUrl, realm, clientId}` (public route, no auth).
- Unchanged: API routes + `GET /:code` redirect. 7-char base62 codes never collide with `static` / `app-config.json` / `healthz` / `auth` / `users` / `api` / `swagger`, and Echo matches those specific routes before the param route.

### Config without hardcoding (DRY)
`GET /app-config.json` derives values from the backend's existing `KEYCLOAK_*` env, so the SPA never drifts from the backend and no rebuild is needed to change realm/client:
- `authUrl` + `realm` parsed from `KEYCLOAK_ISSUER` (`{authUrl}/realms/{realm}` → split on `/realms/`).
- `clientId` = `KEYCLOAK_CLIENT_ID` (`go-shortener`).
These are public client values (public client, no secret) — safe to expose.

### Auth flow (`static/app.js`, keycloak-js)
1. `fetch('/app-config.json')` → `{authUrl, realm, clientId}`.
2. `kc = new Keycloak({url, realm, clientId})`; `await kc.init({ onLoad: 'check-sso', pkceMethod: 'S256', checkLoginIframe: false })`.
3. Signed out → render a **Sign in** button → `kc.login({ redirectUri: location.origin + '/' })`.
4. Signed in → `api()` helper attaches the token and refreshes it:
   ```js
   async function api(path, opts = {}) {
     await kc.updateToken(30);
     return fetch(path, { ...opts, headers: { ...opts.headers, Authorization: 'Bearer ' + kc.token } });
   }
   ```
5. Load profile via `api('/auth/me')`; create link via `api('/api/links', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({url, expires_at})})`.
6. Sign out → `kc.logout({ redirectUri: location.origin })`.

### Pages / UI (single page, minimal CSS)
- **Signed out:** title + "Sign in with Keycloak".
- **Signed in:** greeting (username/email from `/auth/me`); a create-link form (URL, optional expiry) → on `201` show the `short_url` with a **Copy** button; on `429` show "daily quota exceeded"; on `400` show the API message. Optional: a "look up stats" input (code → `GET /api/links/:code/stats` → show click count). A **Sign out** link.

## Interface Contracts

- `GET /app-config.json` → `200 { "authUrl": "https://auth.cd.me", "realm": "nine-realms", "clientId": "go-shortener" }`.
- Frontend → API: unchanged existing contracts (`/auth/me`, `POST /api/links`, `GET /api/links/:code/stats`) with `Authorization: Bearer`.

## Error Handling
- API errors surface the envelope `error.message` to the user; `429` → quota message; `401` (token expired mid-session) → prompt re-login.
- keycloak-js init failure (Keycloak unreachable) → show a non-blocking "auth unavailable" message; the page still renders.
- Never `innerHTML` untrusted/API data — use `textContent` to avoid XSS (token lives in memory).

## Keycloak setup (ops, no code)
- On the `go-shortener` client: add the Go server origin to **Valid redirect URIs** (`http://localhost:8080/*`, prod domain) and **Web origins** (the origin, for the Keycloak CORS the JS adapter needs). Standard flow + PKCE (S256) enabled; it's a public client.
- No backend CORS needed (frontend and API are same origin); the only cross-origin is browser → Keycloak, handled by Web Origins.

## Testing Strategy
- **Go handler tests:** `/app-config.json` returns correctly parsed `authUrl`/`realm`/`clientId` from a sample `KEYCLOAK_ISSUER`; `/` serves `index.html`; `/static/app.js` serves; a `/:code` redirect still resolves (no regression from the new routes).
- **Manual E2E:** sign-in redirect → create link → copy → sign out (no JS unit harness in a Go repo; out of scope).
- Gate: `make build` + `make test` green.

## Files
- **Create:** `web/index.html`, `web/static/app.js`, `web/static/styles.css`, `web/static/keycloak.min.js` (vendored, matching the Keycloak server major version), `web/embed.go`, `internal/handler/frontend_handler.go` (serves `/`, `/app-config.json`; parses issuer).
- **Modify:** `internal/router/router.go` (register `/`, `/static`, `/app-config.json`; pass embed.FS + Keycloak cfg), `cmd/server/main.go` (wiring), `README.md` (frontend section + Keycloak client setup note).
- **No new env, no migration.**

## Risks
- **Route precedence vs `/:code`:** the new exact/prefix routes must register so they win; add the no-regression redirect test. Low risk (Echo prioritizes them).
- **keycloak-js / server version drift:** vendor keycloak-js matching the server major; note in README.
- **`check-sso` without a silent-check iframe:** using `checkLoginIframe:false` avoids third-party-cookie/iframe issues; trade-off is no background session sync (fine for a tiny app).
- **Token in memory:** acceptable for a public SPA; mitigated by `textContent`-only rendering and short access-token TTL.
- **Binary size:** embedding static assets grows the binary marginally; negligible.

## Success Criteria
- Visiting the Go server origin serves the page; "Sign in" redirects to Keycloak and back; signed-in user sees their profile and can create + copy a short link; `429`/`400` handled; sign out works.
- The existing `/:code` redirect and all API routes still work.
- One binary, no API CORS, no build step. `make build` + `make test` green.

## Open Questions
- Optional stats-by-code box: include in MVP or defer? (Assumed included as a tiny extra; trivial to drop.)
- Vendored keycloak-js version — confirm the Keycloak server version to match the adapter major.

## Next Steps
`/plan` (or `/cook` directly — small, mostly static files + one Go handler + router wiring).
