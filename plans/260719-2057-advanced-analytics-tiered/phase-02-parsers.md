# Phase 02 — Parsers (`pkg/useragent`, `pkg/referrer`)

## Context Links
- Overview: [plan.md](plan.md)
- Existing `pkg/` packages for style reference: `pkg/apperror/`, `pkg/response/`, `pkg/database/`

## Overview
- **Priority:** P0 (blocks 03)
- **Status:** pending
- Two small, isolated, unit-testable pure packages: UA → (device, browser, os); referrer → domain.

## Key Insights
- Parse at **write-time** (consumer), once per click. No parsing on read path.
- Keep parsers dependency-light and deterministic → trivial to test.
- Values must match the rollup column widths from phase 01 (device ≤20, browser/os ≤40, domain ≤255).

## Requirements
- **FR-1:** `useragent.Parse(ua string) Result{Device, Browser, OS}` — normalized, lowercased device class.
- **FR-2:** device ∈ {desktop, mobile, tablet, bot, unknown}. Empty UA → all "unknown".
- **FR-3:** `referrer.Domain(ref string) string` — host only; empty/unparseable → "direct".
- **FR-4:** strip `www.` prefix; lowercase; truncate to 255.
- **NFR:** zero network calls; pure functions; no global state.

## Architecture
`useragent` wraps `github.com/mileusna/useragent` (zero-dep, pure Go). `referrer` uses stdlib `net/url` only. Both expose a single small surface so callers (consumer, tests) depend on a stable interface.

## Related Code Files
**Create:**
- `pkg/useragent/useragent.go` (~60 lines)
- `pkg/useragent/useragent_test.go`
- `pkg/referrer/referrer.go` (~40 lines)
- `pkg/referrer/referrer_test.go`

**Modify:**
- `go.mod` / `go.sum` — add `github.com/mileusna/useragent` (`go get`, pin exact version).

## Implementation Steps

1. `go get github.com/mileusna/useragent@latest` (verify it's the real, maintained package before adding — avoid typosquats). Then `go mod tidy`.

2. `pkg/useragent/useragent.go`:
   ```go
   package useragent

   import ua "github.com/mileusna/useragent"

   type Result struct {
       Device  string // desktop|mobile|tablet|bot|unknown
       Browser string // e.g. Chrome; "" -> "unknown"
       OS      string // e.g. Windows; "" -> "unknown"
   }

   func Parse(s string) Result {
       if s == "" {
           return Result{Device: "unknown", Browser: "unknown", OS: "unknown"}
       }
       p := ua.Parse(s)
       return Result{
           Device:  device(p),
           Browser: nonEmpty(truncate(p.Name, 40)),
           OS:      nonEmpty(truncate(p.OS, 40)),
       }
   }

   func device(p ua.UserAgent) string {
       switch {
       case p.Bot:     return "bot"
       case p.Tablet:  return "tablet"
       case p.Mobile:  return "mobile"
       case p.Desktop: return "desktop"
       default:        return "unknown"
       }
   }
   // nonEmpty("") -> "unknown"; truncate caps rune length.
   ```

3. `pkg/referrer/referrer.go`:
   ```go
   package referrer

   import (
       "net/url"
       "strings"
   )

   // Domain returns the lowercased host of ref (www. stripped, ≤255 chars).
   // Empty or unparseable referrers return "direct".
   func Domain(ref string) string {
       ref = strings.TrimSpace(ref)
       if ref == "" {
           return "direct"
       }
       u, err := url.Parse(ref)
       if err != nil || u.Host == "" {
           return "direct"
       }
       h := strings.ToLower(u.Hostname())
       h = strings.TrimPrefix(h, "www.")
       if len(h) > 255 {
           h = h[:255]
       }
       if h == "" {
           return "direct"
       }
       return h
   }
   ```

4. Write table-driven tests covering: empty, desktop Chrome, mobile Safari, Googlebot, garbage string (useragent); empty, full URL, host-only, scheme-less, over-long, `www.` strip (referrer).

## Todo List
- [ ] `go get` + verify package legitimacy + `go mod tidy`
- [ ] Implement `pkg/useragent`
- [ ] Implement `pkg/referrer`
- [ ] Table-driven tests both packages
- [ ] `go build ./...` clean

## Success Criteria
- `go test ./pkg/useragent/... ./pkg/referrer/...` passes.
- All device classes reachable; "direct"/"unknown" fallbacks verified.
- No values exceed rollup column widths.

## Risk Assessment
- **UA lib accuracy:** mileusna/useragent is heuristic; "unknown" fallback is acceptable. Not safety-critical.
- **Dependency risk:** pin exact version; confirm package name to avoid typosquatting (per security rules).

## Security Considerations
- Referrer/UA are untrusted input → treat as opaque data, only extract/normalize, never eval. Truncation prevents oversized-row abuse.

## Next Steps
- Phase 03 consumer calls both parsers per click before rollup upsert.
