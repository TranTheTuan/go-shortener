# Phase 02 — Frontend "My links" UI

**Context:** [plan.md](plan.md) · [spec](../reports/spec-260701-1613-user-links-list.md)

## Overview
- **Priority:** High
- **Status:** pending
- Render the user's links (paginated, with clicks) in the vanilla SPA.

## Related Code Files
- **Modify:** `web/index.html`, `web/static/app.js`, `web/static/styles.css`

## Implementation Steps

1. **`index.html`** — inside `#signed-in`, add a "My links" section:
   ```html
   <hr />
   <section id="links">
     <h2>My links</h2>
     <p id="links-status" class="muted"></p>
     <table id="links-table" hidden>
       <thead><tr><th>Short</th><th>Original</th><th>Clicks</th><th>Created</th><th>Expires</th></tr></thead>
       <tbody id="links-body"></tbody>
     </table>
     <div id="links-pager" class="row" hidden>
       <button id="prev" type="button">Prev</button>
       <span id="page-info" class="muted"></span>
       <button id="next" type="button">Next</button>
     </div>
   </section>
   ```

2. **`app.js`** — add `wireLinks(api)` called from `renderSignedIn`:
   - `const PAGE = 20; let offset = 0;`
   - `loadLinks()`: `api('/api/links?limit=' + PAGE + '&offset=' + offset)` → render.
   - Render rows with `createElement` + `textContent` only (XSS-safe):
     - Short: an `<a>` (href=`short_url`, target=_blank, rel=noopener) + a Copy button (reuse the create form's copy pattern → extract a shared `copyButton(url)` helper, DRY).
     - Original: truncated text with `title` = full URL.
     - Clicks: `total_clicks`.
     - Created / Expires: locale date; Expires shows "expired" when past, "—" when null.
   - Pager: `page-info` = `Showing ${offset+1}–${offset+items.length} of ${total}` (or "No links yet." when total 0); `prev` disabled when `offset===0`; `next` disabled when `offset+PAGE>=total`. Buttons adjust `offset` by ±PAGE and reload.
   - **After a successful create** (`showCreated`), reset `offset=0` and call `loadLinks()` so the new link appears.
   - Errors → `links-status` message; network errors handled like the other forms.

3. **`styles.css`** — minimal table styling (full-width, small font, muted borders, truncation via `max-width` + `text-overflow: ellipsis` on the original-URL cell); style pager row.

## Key Insight
Extract the Copy-button creation into one helper and reuse it in both `showCreated` and the links table (DRY). Keep all data rendering on `textContent`/attributes — never `innerHTML`.

## Todo
- [ ] `index.html` My-links section (table + pager)
- [ ] `app.js` `wireLinks` + `loadLinks` + shared `copyButton` helper
- [ ] reload list after create (offset reset)
- [ ] pager enable/disable logic
- [ ] table + pager CSS

## Success Criteria
- Signed-in user sees their links with clicks; prev/next works; new link shows after create; empty state renders "No links yet."; no `innerHTML` of API data.

## Next
Phase 03 docs.
