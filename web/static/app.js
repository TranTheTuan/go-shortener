// Minimal vanilla frontend for the Go URL Shortener.
//
// Authenticates against Keycloak (Authorization Code + PKCE via keycloak-js),
// then calls the same-origin JSON API with a Bearer token. Runtime config
// (auth URL / realm / client) is fetched from the backend so it never drifts.
import Keycloak from "/static/keycloak.js";
import { wireBulk } from "/static/bulk.js";

const $ = (id) => document.getElementById(id);

// text sets element text safely (never innerHTML — avoids XSS from API/user data).
const text = (id, value) => {
  $(id).textContent = value;
};

// confirmDelete shows a custom modal and resolves true if the user confirms.
const confirmDelete = (shortURL) => new Promise((resolve) => {
  const backdrop = $("confirm-modal");
  $("modal-body").textContent = shortURL;
  backdrop.hidden = false;

  const cleanup = (result) => {
    backdrop.hidden = true;
    off($("modal-confirm"), "click", onOk);
    off($("modal-cancel"), "click", onCancel);
    off(backdrop, "click", onBackdrop);
    resolve(result);
  };
  const onOk = () => cleanup(true);
  const onCancel = () => cleanup(false);
  const onBackdrop = (e) => { if (e.target === backdrop) cleanup(false); };

  $("modal-confirm").addEventListener("click", onOk);
  $("modal-cancel").addEventListener("click", onCancel);
  backdrop.addEventListener("click", onBackdrop);
});
const off = (el, ev, fn) => el.removeEventListener(ev, fn);

async function main() {
  let cfg;
  try {
    cfg = await (await fetch("/app-config.json")).json();
  } catch {
    text("status", "Could not load app configuration.");
    return;
  }

  const kc = new Keycloak({ url: cfg.authUrl, realm: cfg.realm, clientId: cfg.clientId });

  let authenticated = false;
  try {
    authenticated = await kc.init({
      onLoad: "check-sso",
      pkceMethod: "S256",
      checkLoginIframe: false,
    });
  } catch {
    text("status", "Authentication service is unavailable. Try again later.");
    return;
  }

  text("status", "");
  if (authenticated) {
    renderSignedIn(kc, cfg);
  } else {
    renderSignedOut(kc);
  }
}

function renderSignedOut(kc) {
  $("status").hidden = true;
  $("signed-out").hidden = false;
  $("signin").onclick = () => kc.login({ redirectUri: location.origin + "/" });
}

function renderSignedIn(kc, cfg) {
  $("status").hidden = true;
  $("app").hidden = false;
  // Use the same redirect URI shape as login (origin + "/") so it matches the
  // Keycloak client's whitelist — a bare origin can fail post-logout validation.
  $("signout").onclick = () => kc.logout({ redirectUri: location.origin + "/" });
  wireMenu($("user-btn"), $("user-dropdown"));
  wireMenu($("settings-btn"), $("settings-dropdown"));

  // api attaches a fresh Bearer token to a same-origin request.
  const api = async (path, opts = {}) => {
    await kc.updateToken(30);
    return fetch(path, {
      ...opts,
      headers: { ...(opts.headers || {}), Authorization: "Bearer " + kc.token },
    });
  };

  wireNav();
  const links = wireLinks(api);
  loadProfile(api);
  wireCreateForm(api, links.reload);
  wireStatsForm(api);
  wireBulk(api);
  if (cfg.paddleClientToken) wireBilling(api, cfg.paddleClientToken);
}

// wireNav switches between sidebar views and keeps the URL hash in sync so a
// view is deep-linkable and survives a refresh.
function wireNav() {
  const items = document.querySelectorAll(".nav-item");
  const views = document.querySelectorAll(".view");
  const titles = { create: "Create link", links: "My links", stats: "Stats", bulk: "Bulk upload", billing: "Billing" };

  const show = (name) => {
    if (!titles[name]) name = "create";
    items.forEach((b) => b.classList.toggle("active", b.dataset.view === name));
    views.forEach((v) => (v.hidden = v.dataset.view !== name));
    text("view-title", titles[name]);
    if (location.hash !== "#" + name) history.replaceState(null, "", "#" + name);
  };

  const closeSidebar = () => {
    document.querySelector(".sidebar").classList.remove("open");
    $("sidebar-overlay").classList.remove("open");
    $("hamburger").setAttribute("aria-expanded", "false");
  };

  // Close sidebar on nav click (mobile UX).
  items.forEach((b) => (b.onclick = () => { show(b.dataset.view); closeSidebar(); }));

  $("hamburger").addEventListener("click", () => {
    const open = document.querySelector(".sidebar").classList.toggle("open");
    $("sidebar-overlay").classList.toggle("open", open);
    $("hamburger").setAttribute("aria-expanded", String(open));
  });
  $("sidebar-overlay").addEventListener("click", closeSidebar);

  show(location.hash.slice(1));
}

// wireMenu toggles a dropdown for a trigger button and closes it on an outside
// click or Escape. The button and its dropdown share a positioned .menu parent.
function wireMenu(btn, dropdown) {
  const setOpen = (open) => {
    dropdown.hidden = !open;
    btn.setAttribute("aria-expanded", String(open));
  };
  btn.onclick = (e) => {
    e.stopPropagation();
    setOpen(dropdown.hidden);
  };
  document.addEventListener("click", (e) => {
    if (!btn.contains(e.target) && !dropdown.contains(e.target)) setOpen(false);
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") setOpen(false);
  });
}

async function loadProfile(api) {
  try {
    const res = await api("/auth/me");
    if (!res.ok) return;
    const { data } = await res.json();
    // Fill the avatar initial + username (XSS-safe via textContent).
    $("avatar").textContent = (data.username || "?").charAt(0);
    const name = $("user-name");
    name.textContent = data.username;
    name.title = data.email;
  } catch {
    /* non-fatal */
  }
}

function wireCreateForm(api, onCreated) {
  $("create-form").onsubmit = async (ev) => {
    ev.preventDefault();
    const url = $("url").value.trim();
    if (!url) return;

    const body = { url };
    const expires = $("expires").value;
    if (expires) body.expires_at = new Date(expires).toISOString();

    text("result", "Creating…");
    let res, json;
    try {
      res = await api("/api/links", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      json = await res.json().catch(() => ({}));
    } catch {
      text("result", "Network error — please retry.");
      return;
    }

    if (res.ok && json.data?.short_url) {
      showCreated(json.data.short_url);
      onCreated?.(); // refresh the links list so the new link appears
    } else if (res.status === 429) {
      text("result", "Daily quota exceeded — try again tomorrow.");
    } else {
      text("result", "Error: " + (json.error?.message || res.status));
    }
  };
}

// linkAnchor returns an <a> to a short URL (new tab, XSS-safe via textContent).
function linkAnchor(shortURL) {
  const a = document.createElement("a");
  a.href = shortURL;
  a.textContent = shortURL;
  a.target = "_blank";
  a.rel = "noopener";
  return a;
}

// copyButton returns a button that copies value to the clipboard on click.
function copyButton(value) {
  const btn = document.createElement("button");
  btn.type = "button";
  btn.textContent = "Copy";
  btn.onclick = async () => {
    await navigator.clipboard.writeText(value);
    btn.textContent = "Copied!";
    setTimeout(() => (btn.textContent = "Copy"), 1500);
  };
  return btn;
}

function showCreated(shortURL) {
  const result = $("result");
  result.textContent = "";
  result.append(linkAnchor(shortURL), copyButton(shortURL));
}

function wireStatsForm(api) {
  $("stats-form").onsubmit = async (ev) => {
    ev.preventDefault();
    const raw = $("code").value.trim();
    if (!raw) return;
    // Extract code from full URL: "https://host/Ab3xY7q" → "Ab3xY7q"
    const code = raw.includes("/") ? raw.split("/").filter(Boolean).pop() : raw;

    text("stats-result", "Loading…");
    $("stats-clicks-table").hidden = true;
    let res, json;
    try {
      res = await api("/api/links/" + encodeURIComponent(code) + "/stats");
      json = await res.json().catch(() => ({}));
    } catch {
      text("stats-result", "Network error — please retry.");
      return;
    }

    if (res.ok) {
      const d = json.data;
      text("stats-result", `Total clicks: ${d.total_clicks}`);
      const clicks = d.recent_clicks ?? [];
      if (clicks.length) {
        const body = $("stats-clicks-body");
        body.textContent = "";
        for (const c of clicks) {
          const tr = document.createElement("tr");
          [
            new Date(c.clicked_at).toLocaleString(),
            c.referrer || "—",
            c.ip_address || "—",
          ].forEach((v) => {
            const td = document.createElement("td");
            td.textContent = v;
            tr.append(td);
          });
          body.append(tr);
        }
        $("stats-clicks-table").hidden = false;
      }
    } else {
      text("stats-result", "Error: " + (json.error?.message || res.status));
    }
  };
}

// expiryLabel renders an expiry cell: "—" when null, "expired" when past.
function expiryLabel(expiresAt) {
  if (!expiresAt) return "—";
  const d = new Date(expiresAt);
  return d.getTime() < Date.now() ? "expired" : d.toLocaleDateString();
}

// statusOf derives display status with precedence disabled > expired > active.
function statusOf(it) {
  if (!it.is_active) return "disabled";
  if (it.expires_at && new Date(it.expires_at).getTime() < Date.now()) return "expired";
  return "active";
}

// statusBadge builds a colored status pill.
function statusBadge(kind) {
  const span = document.createElement("span");
  span.className = "badge badge-" + kind;
  span.textContent = kind;
  return span;
}

// actionBtn is a small labelled row-action button.
function actionBtn(label, onClick) {
  const b = document.createElement("button");
  b.type = "button";
  b.className = "action";
  b.textContent = label;
  b.onclick = onClick;
  return b;
}

// toLocalInput converts an ISO timestamp to a datetime-local input value (local tz).
function toLocalInput(iso) {
  const d = new Date(iso);
  return new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}

// wireLinks renders the user's links (filtered + paginated) with row actions,
// and returns { reload } so a new link created elsewhere refreshes the list.
function wireLinks(api) {
  const PAGE = 20;
  let offset = 0;
  let total = 0;
  let status = "";

  // save PUTs the full mutable state (expiry + active); patch overrides fields.
  const save = async (it, patch) => {
    const body = { is_active: it.is_active, expires_at: it.expires_at ?? null, ...patch };
    let res;
    try {
      res = await api("/api/links/" + encodeURIComponent(it.short_code), {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
    } catch {
      text("links-status", "Network error — please retry.");
      return;
    }
    if (!res.ok) {
      const j = await res.json().catch(() => ({}));
      text("links-status", "Error: " + (j.error?.message || res.status));
      return;
    }
    load();
  };

  const remove = async (it) => {
    if (!await confirmDelete(it.short_url)) return;
    let res;
    try {
      res = await api("/api/links/" + encodeURIComponent(it.short_code), { method: "DELETE" });
    } catch {
      text("links-status", "Network error — please retry.");
      return;
    }
    if (res.status !== 204 && !res.ok) {
      const j = await res.json().catch(() => ({}));
      text("links-status", "Error: " + (j.error?.message || res.status));
      return;
    }
    if (offset > 0 && total - 1 <= offset) offset -= PAGE; // step back if page emptied
    load();
  };

  // One global listener closes all open row dropdowns on outside click.
  document.addEventListener("click", () => {
    document.querySelectorAll(".row-dropdown:not([hidden])").forEach((d) => (d.hidden = true));
  });

  const render = (items) => {
    const body = $("links-body");
    body.textContent = "";
    for (const it of items) {
      const tr = document.createElement("tr");

      // Short: display code only; href goes through the shortener redirect.
      const short = document.createElement("td");
      const a = document.createElement("a");
      a.href = it.short_url; a.textContent = it.short_code;
      a.target = "_blank"; a.rel = "noopener";
      short.append(a, copyButton(it.short_url));
      tr.append(short);

      // Original — hidden on narrow screens via .col-original.
      const original = document.createElement("td");
      original.className = "truncate col-original";
      original.textContent = it.original_url; original.title = it.original_url;
      tr.append(original);

      const clicks = document.createElement("td");
      clicks.textContent = it.total_clicks;
      tr.append(clicks);

      const st = document.createElement("td");
      st.append(statusBadge(statusOf(it)));
      tr.append(st);

      // Expires label only — hidden on narrow screens via .col-expires.
      const exp = document.createElement("td");
      exp.className = "col-expires";
      exp.textContent = expiryLabel(it.expires_at);
      tr.append(exp);

      // Actions: single ⋮ dropdown for all three operations.
      const actions = document.createElement("td");
      actions.className = "actions";
      const menu = document.createElement("div");
      menu.className = "menu";
      const menuBtn = document.createElement("button");
      menuBtn.type = "button";
      menuBtn.className = "action-menu-btn";
      menuBtn.setAttribute("aria-haspopup", "true");
      menuBtn.setAttribute("aria-expanded", "false");
      menuBtn.textContent = "⋮";
      const drop = document.createElement("div");
      drop.className = "dropdown row-dropdown";
      drop.hidden = true;

      menuBtn.onclick = (e) => {
        e.stopPropagation();
        const wasOpen = !drop.hidden;
        document.querySelectorAll(".row-dropdown:not([hidden])").forEach((d) => (d.hidden = true));
        drop.hidden = wasOpen;
        menuBtn.setAttribute("aria-expanded", String(!wasOpen));
      };

      const close = () => { drop.hidden = true; menuBtn.setAttribute("aria-expanded", "false"); };

      // Toggle enable/disable.
      const toggleItem = document.createElement("button");
      toggleItem.type = "button"; toggleItem.className = "dropdown-item";
      toggleItem.textContent = it.is_active ? "Disable" : "Enable";
      toggleItem.onclick = () => { close(); save(it, { is_active: !it.is_active }); };

      // Edit expires — inserts an inline row below.
      const expiresItem = document.createElement("button");
      expiresItem.type = "button"; expiresItem.className = "dropdown-item";
      expiresItem.textContent = "Edit expires";
      expiresItem.onclick = () => {
        close();
        const next = tr.nextElementSibling;
        if (next?.classList.contains("edit-expires-row")) { next.remove(); return; }
        const editTr = document.createElement("tr");
        editTr.className = "edit-expires-row";
        const editTd = document.createElement("td");
        editTd.colSpan = 6;
        const input = document.createElement("input");
        input.type = "datetime-local";
        if (it.expires_at) input.value = toLocalInput(it.expires_at);
        editTd.append(
          input,
          actionBtn("Save", () => save(it, { expires_at: input.value ? new Date(input.value).toISOString() : null })),
          actionBtn("Clear", () => save(it, { expires_at: null })),
          actionBtn("Cancel", () => editTr.remove()),
        );
        editTr.append(editTd);
        tr.after(editTr);
      };

      // Delete.
      const deleteItem = document.createElement("button");
      deleteItem.type = "button"; deleteItem.className = "dropdown-item danger";
      deleteItem.textContent = "Delete";
      deleteItem.onclick = () => { close(); remove(it); };

      drop.append(toggleItem, expiresItem, deleteItem);
      menu.append(menuBtn, drop);
      actions.append(menu);
      tr.append(actions);
      body.append(tr);
    }
  };

  async function load() {
    text("links-status", "Loading…");
    let res, json;
    try {
      const q = `/api/links?limit=${PAGE}&offset=${offset}` + (status ? `&status=${status}` : "");
      res = await api(q);
      json = await res.json().catch(() => ({}));
    } catch {
      text("links-status", "Could not load your links.");
      return;
    }
    if (!res.ok) {
      text("links-status", "Error: " + (json.error?.message || res.status));
      return;
    }

    const items = json.data?.items ?? [];
    total = json.data?.total ?? 0;
    if (total === 0) {
      text("links-status", status ? "No links with this status." : "No links yet.");
      $("links-table").hidden = true;
      $("links-pager").hidden = true;
      return;
    }

    text("links-status", "");
    render(items);
    $("links-table").hidden = false;
    $("links-pager").hidden = false;
    text("page-info", `Showing ${offset + 1}–${offset + items.length} of ${total}`);
    $("prev").disabled = offset === 0;
    $("next").disabled = offset + PAGE >= total;
  }

  $("links-filter").onchange = () => {
    status = $("links-filter").value;
    offset = 0;
    load();
  };
  $("prev").onclick = () => {
    if (offset > 0) {
      offset -= PAGE;
      load();
    }
  };
  $("next").onclick = () => {
    if (offset + PAGE < total) {
      offset += PAGE;
      load();
    }
  };

  load();
  return {
    reload: () => {
      offset = 0;
      load();
    },
  };
}

main();

// PLAN display metadata (cosmetic only — no price IDs here).
const PLAN_META = {
  basic: { desc: "Get started for free", features: ["10 links / day", "Click stats", "Custom expiry"] },
  pro: { desc: "For power users", features: ["500 links / day", "Bulk upload", "Click stats", "Custom expiry"] },
  business: { desc: "Unlimited links", features: ["Unlimited links / day", "Bulk upload", "Priority support", "Click stats"] },
};
const PLAN_RANK = { basic: 0, pro: 1, business: 2 };

function wireBilling(api, paddleClientToken) {
  const navBtn = $("billing-nav");
  if (navBtn) navBtn.hidden = false;

  let paddleReady = false;

  const script = document.createElement("script");
  script.src = "https://cdn.paddle.com/paddle/v2/paddle.js";
  script.onload = () => {
    Paddle.Environment.set('sandbox');
    Paddle.Initialize({
      token: paddleClientToken,
      eventCallback(e) {
        if (e.name === "checkout.completed") load();
      },
    });
    paddleReady = true;
  };
  document.head.appendChild(script);

  async function load() {
    $("billing-loading").hidden = false;
    $("billing-content").hidden = true;

    let subRes, subJson, plansRes, plansJson, meJson;
    try {
      [subRes, plansRes] = await Promise.all([api("/api/subscription"), fetch("/api/plans")]);
      const meRes = await api("/auth/me");
      [subJson, plansJson, meJson] = await Promise.all([
        subRes.json().catch(() => ({})),
        plansRes.json().catch(() => ({})),
        meRes.json().catch(() => ({})),
      ]);
    } catch {
      $("billing-loading").textContent = "Could not load plan info.";
      return;
    }
    if (!subRes.ok) {
      $("billing-loading").textContent = "Error: " + (subJson.error?.message || subRes.status);
      return;
    }

    const plans = plansJson.data ?? [];
    const userEmail = meJson.data?.email ?? null;

    $("billing-loading").hidden = true;
    $("billing-content").hidden = false;
    renderCurrentPlan(subJson.data);
    renderPlanGrid(subJson.data, plans, userEmail);
  }

  function renderCurrentPlan(data) {
    const card = $("current-plan-card");
    const planCode = data.plan?.code ?? "basic";
    const quotaRemaining = data.quota_remaining;
    const sub = data.subscription;
    const meta = PLAN_META[planCode] ?? PLAN_META.basic;

    card.innerHTML = "";

    const header = document.createElement("div");
    header.className = "plan-card-header";

    const nameWrap = document.createElement("div");
    const nameEl = document.createElement("span");
    nameEl.className = "plan-name";
    nameEl.textContent = data.plan?.name ?? "Basic";
    const badge = document.createElement("span");
    badge.className = "badge badge-active plan-current-badge";
    badge.textContent = "current";
    nameWrap.append(nameEl, badge);

    const quota = document.createElement("div");
    quota.className = "plan-quota";
    quota.textContent = quotaRemaining === 2147483647
      ? "Unlimited links remaining today"
      : `${quotaRemaining} link${quotaRemaining !== 1 ? "s" : ""} remaining today`;

    header.append(nameWrap, quota);
    card.append(header);

    const renewsText = sub?.current_period_end
      ? "Renews " + new Date(sub.current_period_end).toLocaleDateString()
      : "";
    const canceledText = sub?.canceled_at ? "Cancels at period end" : "";
    if (renewsText || canceledText) {
      const p = document.createElement("p");
      p.className = "plan-meta-text";
      p.textContent = canceledText || renewsText;
      card.append(p);
    }

    if (sub?.paddle_customer_id) {
      const btn = document.createElement("button");
      btn.textContent = "Manage subscription →";
      btn.className = "portal-btn";
      btn.onclick = async () => {
        btn.disabled = true;
        btn.textContent = "Opening portal…";
        try {
          const res = await api("/api/subscription/portal");
          const json = await res.json().catch(() => ({}));
          if (res.ok && json.data?.url) {
            window.open(json.data.url, "_blank", "noopener");
          } else {
            alert("Could not open portal: " + (json.error?.message || res.status));
          }
        } catch {
          alert("Network error — please retry.");
        } finally {
          btn.disabled = false;
          btn.textContent = "Manage subscription →";
        }
      };
      card.append(btn);
    }
  }

  function renderPlanGrid(data, plans, userEmail) {
    const grid = $("plan-grid");
    grid.innerHTML = "";
    const currentCode = data.plan?.code ?? "basic";
    const currentInterval = data.subscription?.billing_interval ?? "monthly";
    const paddleCustomerId = data.subscription?.paddle_customer_id ?? null;
    const userId = data.subscription?.user_id ?? null;

    const toggle = document.createElement("div");
    toggle.className = "interval-toggle";
    let activeInterval = currentInterval;

    ["monthly", "yearly"].forEach((iv) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.textContent = iv === "yearly" ? "Yearly (save ~27%)" : "Monthly";
      btn.className = "interval-btn" + (iv === activeInterval ? " active" : "");
      btn.dataset.interval = iv;
      btn.onclick = () => {
        toggle.querySelectorAll(".interval-btn").forEach((b) => b.classList.remove("active"));
        btn.classList.add("active");
        activeInterval = iv;
        renderCards(iv);
      };
      toggle.append(btn);
    });
    grid.append(toggle);

    const cardsContainer = document.createElement("div");
    cardsContainer.className = "plan-cards";
    grid.append(cardsContainer);

    function renderCards(iv) {
      cardsContainer.innerHTML = "";
      for (const plan of plans) {
        if (plan.code === "basic") continue;
        const meta = PLAN_META[plan.code] ?? { desc: "", features: [] };
        const priceId = iv === "yearly" ? plan.paddle_price_id_yearly : plan.paddle_price_id_monthly;
        const priceCents = plan.price_cents;
        const priceDisplay = priceCents === 0 ? "Free"
          : iv === "yearly" ? `$${Math.round(priceCents * 10 / 100)}/yr`
            : `$${(priceCents / 100).toFixed(0)}/mo`;

        const card = document.createElement("div");
        card.className = "plan-option" + (plan.code === currentCode ? " plan-option-current" : "");

        const hd = document.createElement("div");
        hd.className = "plan-option-header";
        const nm = document.createElement("span");
        nm.className = "plan-option-name";
        nm.textContent = plan.name;
        const pr = document.createElement("span");
        pr.className = "plan-option-price";
        pr.textContent = priceDisplay;
        hd.append(nm, pr);

        const desc = document.createElement("p");
        desc.className = "plan-option-desc";
        desc.textContent = meta.desc;

        const ul = document.createElement("ul");
        ul.className = "plan-features";
        for (const f of meta.features) {
          const li = document.createElement("li");
          li.textContent = f;
          ul.append(li);
        }

        const btn = document.createElement("button");
        const isCurrent = plan.code === currentCode;
        const isDowngrade = (PLAN_RANK[plan.code] ?? 0) < (PLAN_RANK[currentCode] ?? 0);
        btn.className = isCurrent ? "plan-btn plan-btn-current" : "plan-btn primary";
        btn.disabled = isCurrent || isDowngrade;
        btn.textContent = isCurrent ? "Current plan" : isDowngrade ? "Downgrade not supported" : "Upgrade";
        if (!isCurrent && !isDowngrade && priceId) {
          btn.onclick = () => openCheckout(priceId, paddleCustomerId, userEmail, userId);
        }

        card.append(hd, desc, ul, btn);
        cardsContainer.append(card);
      }
    }

    renderCards(activeInterval);
  }

  function openCheckout(priceId, paddleCustomerId, userEmail, userId) {
    if (!paddleReady) {
      alert("Paddle is still loading, please try again in a moment.");
      return;
    }
    const customer = paddleCustomerId
      ? { id: paddleCustomerId }
      : userEmail ? { email: userEmail } : undefined;
    Paddle.Checkout.open({
      items: [{ priceId, quantity: 1 }],
      customer,
      settings: { successUrl: location.origin + "/#billing" },
      customData: { user_id: userId },
    });
  }

  document.querySelectorAll(".nav-item").forEach((btn) => {
    if (btn.dataset.view === "billing") btn.addEventListener("click", load);
  });

  if (location.hash === "#billing") load();
}
