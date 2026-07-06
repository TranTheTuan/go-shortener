# Phase 05 — Frontend: status + row actions

## Overview
- **Priority:** P1 · **Status:** ✅ done · Depends on: 04
- Add Status + Actions to the "My links" table: Enable/Disable, Edit expiry, Delete.

## Related files
- Modify: `web/index.html` (links table header)
- Modify: `web/static/app.js` (`wireLinks` render + new PUT/DELETE calls)

## Steps
1. **Table header** (`#links-table`) — add columns:
   `Short | Original | Clicks | Status | Expires | Actions`
   (Drop or keep "Created" — replace with Status to avoid too many columns; keep Expires since it's now editable.)
2. **Row render** (`wireLinks` `render`) — per link:
   - **Status cell**: derive `Disabled` if `!is_active`, else `Expired` if past `expires_at`, else `Active`. Small badge (`.badge.active/.disabled/.expired`).
   - **Actions cell** (XSS-safe `createElement` only):
     - `Enable`/`Disable` button → `PUT /api/links/:code` with `{ is_active: !current, expires_at: <current expires_at or null> }`.
     - `Edit expiry`: a small `<input type="datetime-local">` + `Save` (or reuse a prompt) → `PUT` with `{ is_active: current, expires_at: <new ISO or null> }`. Keep it lightweight — inline input revealed on click.
     - `Delete` → `confirm("Delete this link?")` then `DELETE /api/links/:code`; on 204 reload.
   - After any success → `reload()` (already exposed by `wireLinks`).
3. **Status filter control** — above the table, a `<select id="links-filter">` with All / Active / Disabled / Expired. On change → set current filter, reset `offset = 0`, `load()`. `wireLinks.load()` appends `&status=<value>` (omit when "all"). Reset paging when the filter changes so counts/pages stay consistent with the filtered `total`.
4. **API helpers** — reuse the `api(path, opts)` Bearer fetch:
   - PUT: `api(url, { method:"PUT", headers:{"Content-Type":"application/json"}, body: JSON.stringify({expires_at, is_active}) })`.
   - DELETE: `api(url, { method:"DELETE" })`; treat 204 as success.
   - Error → show inline message in `#links-status` (mirror existing error handling).
5. **Styles** (`styles.css`) — add `.badge` variants (active=green/accent, disabled=muted, expired=faint) and compact action-button styling in the mono/editorial system already defined.

## Todo
- [x] header columns (Status, Actions)
- [x] status filter `<select>` (reset offset on change; append `&status=`)
- [x] status badge logic (disabled > expired > active precedence)
- [x] Enable/Disable via PUT
- [x] Edit expiry via PUT (set + clear)
- [x] Delete via DELETE + confirm
- [x] reload after mutation; inline errors
- [x] badge/action CSS

## Success criteria
- Toggling Disable flips the badge and (verified separately) the redirect 410s.
- Editing expiry updates the Expires cell; clearing makes it permanent ("—").
- Delete removes the row after confirm.
- All DOM built with `textContent`/`createElement` (no innerHTML).

## Notes
- `expires_at` for `datetime-local`: strip to `YYYY-MM-DDTHH:mm`; send back as `new Date(v).toISOString()` or `null` when empty. Matches `wireCreateForm`'s existing conversion.
