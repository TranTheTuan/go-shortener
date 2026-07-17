# Phase 03 — Frontend: app.js button condition + API call

**Status:** pending  
**Priority:** high  
**Effort:** small  
**Blocked by:** Phase 01

## Context

- Handler: `web/static/app.js:714-789` (`renderCards` function)
- `currentInterval` is stored as `"monthly"/"yearly"` (line 685)
- API expects `"month"/"year"` (Paddle convention)
- `isCurrent` check only compares plan code, not interval (line 752)

## Files to modify

- `web/static/app.js`

## Problems to fix

| Line | Problem |
|------|---------|
| 752 | `isCurrent` ignores interval — same plan + different interval gets "Current plan" / disabled |
| 763–767 | API body only sends `plan_id`, missing `interval` |
| 685 | `currentInterval` is `"monthly"/"yearly"` but Paddle/API uses `"month"/"year"` |

## Implementation steps

1. Normalize `currentInterval` to Paddle convention at the point it's read (line 685):
   ```js
   const currentInterval = (data.subscription?.billing_interval ?? "month") === "year" ? "year" : "month";
   ```
   Note: `billing_interval` on the JSON is already `"month"/"year"` from Go — the display toggle uses `"monthly"/"yearly"` internally. Keep `activeInterval` as `"monthly"/"yearly"` for the toggle UI; derive the API value separately.

2. Inside `renderCards(iv)`, compute the API interval from the toggle value:
   ```js
   const apiInterval = iv === "yearly" ? "year" : "month";
   ```

3. Fix `isCurrent` — same plan AND same interval:
   ```js
   const isCurrent = plan.code === currentCode && apiInterval === currentInterval;
   ```

4. Fix the no-op case label/disabled — same plan, different interval should show "Switch to yearly" / "Switch to monthly", not "Current plan":
   ```js
   const isSamePlanDiffInterval = plan.code === currentCode && apiInterval !== currentInterval;
   btn.disabled = isDowngrade;
   btn.textContent = isDowngrade
     ? "Downgrade not supported"
     : isCurrent
       ? "Current plan"
       : isSamePlanDiffInterval
         ? (iv === "yearly" ? "Switch to yearly" : "Switch to monthly")
         : "Upgrade";
   btn.className = isCurrent ? "plan-btn plan-btn-current" : "plan-btn primary";
   ```

5. Update the API call to include `interval`:
   ```js
   body: JSON.stringify({ plan_id: plan.id, interval: apiInterval }),
   ```

6. Update the upgrading/upgraded text for interval-only changes:
   ```js
   btn.textContent = isSamePlanDiffInterval ? "Switching…" : "Upgrading…";
   // on success:
   btn.textContent = isSamePlanDiffInterval ? "Switched!" : "Upgraded!";
   ```

## Todo

- [ ] Derive `currentInterval` as `"month"/"year"` from subscription JSON
- [ ] Compute `apiInterval` inside `renderCards`
- [ ] Fix `isCurrent` to include interval comparison
- [ ] Add `isSamePlanDiffInterval` flag
- [ ] Fix button `disabled`, `textContent`, `className` logic
- [ ] Add `interval` to API call body
- [ ] Update in-flight/success button labels

## Success criteria

- Same plan, same interval → "Current plan", disabled
- Same plan, different interval → "Switch to yearly/monthly", enabled, calls API with correct interval
- Higher tier, any interval → "Upgrade", enabled
- Lower tier → "Downgrade not supported", disabled
