# Phase 03 — Terms & Conditions HTML Content

**Status**: pending  
**Priority**: high  
**Effort**: 1 hour  
**Blocked by**: Phase 02

## Context

Write the static T&C HTML page explaining billing rules, data practices, link expiry, and acceptable use. This is served at `GET /terms/v1.html` and embedded into the binary.

## Implementation

### Create: `web/terms/v1.html`

This is a standalone HTML page (not a component). It includes basic styling via CSS classes for dark/light theme compatibility. Use the same font stack as the main app for consistency.

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>Terms of Service — go/short</title>
  <link rel="preconnect" href="https://fonts.googleapis.com" />
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin />
  <link href="https://fonts.googleapis.com/css2?family=Bricolage+Grotesque:opsz,wght@12..96,400..800&family=JetBrains+Mono:wght@400;500;600&display=swap" rel="stylesheet" />
  <style>
    * { margin: 0; padding: 0; box-sizing: border-box; }
    body {
      font-family: 'Bricolage Grotesque', sans-serif;
      line-height: 1.6;
      color: var(--text-color);
      background: var(--bg-color);
      padding: 2rem;
    }
    :root {
      --text-color: #333;
      --bg-color: #fafafa;
      --border-color: #ddd;
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --text-color: #e0e0e0;
        --bg-color: #121212;
        --border-color: #333;
      }
    }
    .container { max-width: 800px; margin: 0 auto; }
    h1 { margin: 2rem 0 1rem; font-size: 2rem; }
    h2 { margin: 1.5rem 0 0.5rem; font-size: 1.5rem; }
    h3 { margin: 1rem 0 0.5rem; font-size: 1.1rem; }
    p { margin-bottom: 1rem; }
    ul, ol { margin-left: 2rem; margin-bottom: 1rem; }
    li { margin-bottom: 0.5rem; }
    strong { font-weight: 600; }
    .version { color: #999; font-size: 0.9rem; margin-top: 2rem; border-top: 1px solid var(--border-color); padding-top: 1rem; }
  </style>
</head>
<body>
  <div class="container">
    <h1>Terms of Service</h1>
    <p><strong>go/short — URL Shortening Service</strong></p>
    <p>Last updated: July 17, 2026</p>

    <h2>1. Service Overview</h2>
    <p><strong>go/short</strong> is a SaaS URL shortening service. Users create shortened links, track click statistics, and manage link expiry. The service is provided on an "as-is" basis under these terms.</p>

    <h2>2. Billing & Subscriptions</h2>
    <h3>2.1 Plan Tiers</h3>
    <ul>
      <li><strong>Basic</strong> (free): 1 short link per day</li>
      <li><strong>Pro</strong> ($9/month or $90/year): 50 links per day</li>
      <li><strong>Business</strong> ($29/month or $290/year): Unlimited links</li>
    </ul>

    <h3>2.2 Upgrades</h3>
    <ul>
      <li>You may upgrade to a higher tier at any time.</li>
      <li><strong>Downgrades are not supported.</strong> To downgrade, please cancel your subscription via the Paddle Customer Portal and subscribe to a lower tier as a new subscription.</li>
      <li>Billing occurs immediately upon upgrade, prorated for the current period.</li>
    </ul>

    <h3>2.3 Billing Interval Changes</h3>
    <ul>
      <li>You may switch between monthly and yearly billing for your current plan tier at any time.</li>
      <li>When switching from <strong>yearly to monthly</strong>, any unused yearly credit is converted into a store credit applied to future monthly charges.</li>
      <li><strong>No refunds are issued.</strong> Interval changes are treated as conversions, not reversals.</li>
    </ul>

    <h3>2.4 Cancellation & No Refunds</h3>
    <ul>
      <li>Subscriptions may be canceled at any time via the Paddle Customer Portal. After cancellation, you revert to the Basic plan.</li>
      <li><strong>No refunds are issued for partial months or unused periods.</strong> Cancellation is effective at the end of your current billing period.</li>
      <li>Re-subscription to a higher tier after cancellation is treated as a new subscription.</li>
    </ul>

    <h2>3. Daily Quota & Link Creation</h2>
    <ul>
      <li>Each plan includes a daily quota of short links (see Plan Tiers above).</li>
      <li>The quota resets at midnight UTC each day.</li>
      <li>When you upgrade plans, your quota resets immediately to reflect the new limit.</li>
      <li>Quota is per-user. Sharing an account to exceed quotas is a violation of acceptable use (see Section 6).</li>
    </ul>

    <h2>4. Link Expiry & Availability</h2>
    <ul>
      <li>You may set an optional expiry date when creating a short link.</li>
      <li>Expired links return HTTP 410 Gone and are not redirected.</li>
      <li>Short links remain available indefinitely if no expiry is set (or until you manually delete them).</li>
      <li>We reserve the right to retire the service and provide 30 days' notice before all links are deleted.</li>
    </ul>

    <h2>5. Data & Privacy</h2>
    <ul>
      <li><strong>Authentication:</strong> You authenticate via Keycloak OIDC. We receive and store your email and username from Keycloak.</li>
      <li><strong>Billing Data:</strong> Billing and subscription data is handled by Paddle, our payment processor. See <a href="https://paddle.com/legal" target="_blank">Paddle's Privacy Policy</a>.</li>
      <li><strong>Click Analytics:</strong> We record the following for each link redirect: click count, referrer (if sent by browser), IP address, and user-agent. This data is retained for the lifetime of the link or 365 days, whichever is shorter.</li>
      <li><strong>Data Deletion:</strong> When you delete a link, all associated click data is permanently deleted. When you cancel your subscription, your data remains available via login until you request deletion.</li>
    </ul>

    <h2>6. Acceptable Use</h2>
    <ul>
      <li>You agree not to use this service for phishing, malware distribution, spam, or illegal activity.</li>
      <li>You agree not to share accounts to bypass quota limits or tier restrictions.</li>
      <li>You agree not to engage in API abuse, denial-of-service attacks, or bulk scraping.</li>
      <li>We reserve the right to suspend or terminate your account for violations without refund.</li>
    </ul>

    <h2>7. Disclaimers & Limitation of Liability</h2>
    <ul>
      <li>The service is provided "as-is" without warranties of any kind.</li>
      <li>We are not liable for data loss, service downtime, or lost revenue arising from use of this service.</li>
      <li>Your sole remedy for service issues is suspension of billing. We do not offer refunds for downtime.</li>
    </ul>

    <h2>8. Changes to These Terms</h2>
    <ul>
      <li>We may update these terms at any time. Material changes will be notified via email and require your re-acceptance.</li>
      <li>Continued use of the service after acceptance constitutes agreement to the new terms.</li>
    </ul>

    <h2>9. Contact</h2>
    <p>For questions or disputes, contact: <strong>support@go-short.local</strong> (or your configured support email)</p>

    <div class="version">
      <p><strong>Version 1.0</strong> — Effective July 17, 2026</p>
    </div>
  </div>
</body>
</html>
```

## Content Notes

1. **Billing clarity**: Explicitly states no downgrades, no refunds on interval changes, no refunds on cancellation. These are the key rules users must understand.
2. **Quota reset behavior**: Clarifies that upgrades trigger immediate reset (prevents user confusion if they upgrade mid-month).
3. **Data practices**: Separate sections for auth, billing, and analytics. Keycloak & Paddle links to their privacy policies.
4. **Acceptable use**: Short list of prohibitions (phishing, spam, abuse) and account sharing clause (prevents quota bypass).
5. **Disclaimers**: Standard liability waiver; we don't offer refunds for downtime.
6. **Versioning footer**: Makes it clear which version they're reading; helps with re-acceptance tracking.

## Styling

- Uses CSS custom properties for dark/light theme support (matches the app's theme toggle)
- Font stack matches `index.html` (Bricolage Grotesque + JetBrains Mono)
- Responsive layout: readable on mobile and desktop
- No external assets except Google Fonts (already loaded by the app)

## Verification

1. File created at `web/terms/v1.html` with valid HTML
2. Can be read and rendered: `curl http://localhost:8000/terms/v1.html` → returns HTML with correct Content-Type
3. Styling renders correctly in browser (checked during Phase 04 gate modal testing)
4. No XSS vulnerabilities: all content is static HTML (no user input)

## Future Versions

When T&C changes significantly:
1. Create `web/terms/v2.html` with updated content
2. Bump `TERMS_VERSION=2.0` in deployment config
3. Existing users with `terms_version=1.0` see the gate again
4. Old `/terms/v1.html` remains available for audit/history
