# Phase 04 — Frontend Gate & Modal

**Status**: pending  
**Priority**: critical  
**Effort**: 1.5 hours  
**Blocked by**: Phase 02 + Phase 03

## Context

Add a terms acceptance gate to the frontend that blocks app access until the user accepts the current T&C version. The gate shows between Keycloak auth success and app shell render.

## Implementation

### 1. Backend: Inject termsVersion into app config

File: `internal/handler/config_handler.go` (or wherever `/app-config.json` is served)

**Update the config response** to include terms version. Find the existing handler that returns app config and add:

```go
type appConfig struct {
    AuthURL              string `json:"auth_url"`
    Realm                string `json:"realm"`
    ClientID             string `json:"client_id"`
    PaddleClientToken    string `json:"paddle_client_token,omitempty"`
    TermsVersion         string `json:"terms_version"` // ADD THIS
}
```

Then populate it in the handler:
```go
config := appConfig{
    AuthURL:           cfg.Keycloak.IssuerURL, // or similar
    Realm:             cfg.Keycloak.Realm,
    ClientID:          cfg.Keycloak.ClientID,
    PaddleClientToken: cfg.Paddle.ClientToken,
    TermsVersion:      cfg.Terms.CurrentVersion, // ADD THIS
}
```

### 2. Frontend: Add modal HTML to index.html

File: `web/index.html`

**Add this modal after the `#signed-out` div** (around line 50, before `#app`):

```html
  <!-- Terms & Conditions acceptance modal (shown between auth + app render) -->
  <div id="terms-modal" class="modal-backdrop" hidden>
    <div class="modal">
      <div class="modal-header">
        <h2>Terms of Service</h2>
      </div>
      <div class="modal-body">
        <p>Before you continue, please read and accept our Terms of Service.</p>
        <p>Key points:</p>
        <ul>
          <li><strong>Upgrades only</strong> — you cannot downgrade plans</li>
          <li><strong>No refunds</strong> — interval changes and cancellations are non-refundable</li>
          <li><strong>Billing interval</strong> — switch between monthly and yearly (unused credit applies as store credit)</li>
          <li><strong>Cancellation</strong> — managed via Paddle Customer Portal; access reverts to Basic plan</li>
        </ul>
        <p><a href="/terms/v1.html" target="_blank" rel="noopener">Read full Terms of Service →</a></p>
        
        <label style="margin-top: 1.5rem; display: flex; align-items: center;">
          <input type="checkbox" id="terms-checkbox" />
          <span style="margin-left: 0.5rem;">I have read and agree to the Terms of Service</span>
        </label>
      </div>
      <div class="modal-footer">
        <button id="terms-accept-btn" class="primary" disabled>Accept & Continue</button>
      </div>
    </div>
  </div>
```

**Add CSS to `web/static/styles.css`** (modal styling, at the end):

```css
/* Terms modal styling */
.modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}

.modal-backdrop[hidden] {
  display: none;
}

.modal {
  background: var(--bg-secondary);
  border-radius: 8px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2);
  max-width: 500px;
  max-height: 80vh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.modal-header {
  padding: 1.5rem;
  border-bottom: 1px solid var(--border-color);
}

.modal-header h2 {
  margin: 0;
  font-size: 1.5rem;
}

.modal-body {
  padding: 1.5rem;
  overflow-y: auto;
  flex: 1;
}

.modal-body p {
  margin-bottom: 1rem;
}

.modal-body ul {
  margin-left: 1.5rem;
  margin-bottom: 1rem;
}

.modal-body li {
  margin-bottom: 0.5rem;
}

.modal-body a {
  color: #0066cc;
  text-decoration: none;
}

.modal-body a:hover {
  text-decoration: underline;
}

.modal-footer {
  padding: 1rem 1.5rem;
  border-top: 1px solid var(--border-color);
  display: flex;
  gap: 0.5rem;
  justify-content: flex-end;
}

.modal-footer button {
  padding: 0.75rem 1.5rem;
  border-radius: 4px;
  border: none;
  font-size: 1rem;
  cursor: pointer;
}

.modal-footer button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.primary {
  background: #0066cc;
  color: white;
}

.primary:not(:disabled):hover {
  background: #0052a3;
}
```

### 3. Frontend: Add gate logic to app.js

File: `web/static/app.js`

**Add helper functions after the `confirmDelete` function** (around line 50):

```js
// checkTermsGate shows the T&C modal if the user hasn't accepted the current version.
// Returns a Promise that resolves true if accepted, false if user closed without accepting.
async function checkTermsGate(api, cfg) {
  const currentVersion = cfg.termsVersion;
  const cached = localStorage.getItem("terms_version");

  // Fast path: user already accepted this version
  if (cached === currentVersion) {
    return true;
  }

  // Show modal and wait for user decision
  return new Promise((resolve) => {
    const modal = $("terms-modal");
    const checkbox = $("terms-checkbox");
    const acceptBtn = $("terms-accept-btn");

    // Enable accept button only when checkbox is ticked
    checkbox.addEventListener("change", () => {
      acceptBtn.disabled = !checkbox.checked;
    });

    // Accept handler
    const onAccept = async () => {
      try {
        // Send acceptance to backend
        const res = await api("/api/terms/accept", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ version: currentVersion }),
        });

        if (!res.ok) {
          const json = await res.json().catch(() => ({}));
          throw new Error(json.error?.message || res.status);
        }

        // Cache the acceptance
        localStorage.setItem("terms_version", currentVersion);

        // Clean up and resolve
        cleanup();
        resolve(true);
      } catch (e) {
        alert("Failed to accept terms: " + e.message);
      }
    };

    const cleanup = () => {
      modal.hidden = true;
      checkbox.checked = false;
      acceptBtn.disabled = true;
      acceptBtn.removeEventListener("click", onAccept);
    };

    // Show modal and wire handler
    modal.hidden = false;
    acceptBtn.addEventListener("click", onAccept);
  });
}
```

**Modify the `main()` function** to call the gate after Keycloak auth:

Find the line `if (authenticated) { renderSignedIn(kc, cfg); }` (around line 65) and replace it with:

```js
  if (authenticated) {
    // Check T&C gate before rendering app
    if (!(await checkTermsGate(api, cfg))) {
      // User rejected or closed modal; don't render app
      text("status", "You must accept the Terms of Service to use this service.");
      return;
    }
    renderSignedIn(kc, cfg);
  } else {
```

**Also add terms version to app-config fetch.** Find where `app-config.json` is fetched (around line 42) and verify `termsVersion` is included. The backend already provides it (Phase 04 step 1).

### 4. Update `main()` to handle async gate

The `main()` function is async, so `await` works. Verify the function signature:

```js
async function main() {
  // ... existing code ...
  if (authenticated) {
    if (!(await checkTermsGate(api, cfg))) {
      // reject
      return;
    }
    renderSignedIn(kc, cfg);
  } else {
    // ...
  }
}
```

## Verification Flow

1. **Build**: `go build ./...` compiles cleanly
2. **Migrate**: `make migrate` to apply `000014` (Phase 01)
3. **Start server**: `go run ./cmd/server`
4. **First-time user flow**:
   - Load `http://localhost:8000`
   - Click "Sign in with Keycloak"
   - Authenticate (test user or actual Keycloak)
   - **Gate appears**: modal shows with checkbox + link to `/terms/v1.html`
   - Verify link to `/terms/v1.html` works in new tab
   - Check the checkbox
   - Click "Accept & Continue"
   - **API call**: `POST /api/terms/accept` with `{"version":"1.0"}` succeeds (204)
   - **App renders**: main app shell loads
   - **localStorage**: Check that `terms_version` is now `"1.0"`
5. **Refresh browser**: Gate does NOT reappear (localStorage hit short-circuits the check)
6. **Version bump test**:
   - Restart server with `TERMS_VERSION=2.0`
   - Refresh browser
   - **Gate reappears**: `localStorage` has `"1.0"` but config has `"2.0"` → mismatch
   - Accept again
   - Verify `localStorage` updated to `"2.0"`
7. **Wrong version rejection**:
   - In DevTools console: `await fetch('/api/terms/accept', { method: 'POST', body: JSON.stringify({version: 'wrong'}), headers: {'Content-Type': 'application/json'} })`
   - Expect 400 `TERMS_VERSION_MISMATCH`

## Edge Cases

- **No localStorage**: If `localStorage` is disabled, gate will show every refresh. This is acceptable; backend is the source of truth.
- **User closes modal**: Gate stays open; user cannot proceed until they accept. (No "Skip" button intentionally.)
- **Network error on accept**: Alert shown, user can retry.
- **Stale app.js cached by browser**: Might not have the new gate logic. Mitigation: cache-busting via build hash (but that's outside this task; static versioning is acceptable for MVP).

## Notes

- Modal is modal-only (not a separate page), so users don't lose their Keycloak session or need a back button.
- Link to `/terms/v1.html` opens in new tab (`target="_blank"`) so users can read while keeping the modal open.
- Checkbox is required (disabled accept button until checked) to ensure conscious acceptance.
- All API calls use the existing `api()` helper, which adds the Bearer token automatically.
