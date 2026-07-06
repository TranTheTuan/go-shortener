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
    renderSignedIn(kc);
  } else {
    renderSignedOut(kc);
  }
}

function renderSignedOut(kc) {
  $("signed-out").hidden = false;
  $("signin").onclick = () => kc.login({ redirectUri: location.origin + "/" });
}

function renderSignedIn(kc) {
  $("signed-in").hidden = false;
  $("signout").hidden = false;
  $("signout").onclick = () => kc.logout({ redirectUri: location.origin });

  // api attaches a fresh Bearer token to a same-origin request.
  const api = async (path, opts = {}) => {
    await kc.updateToken(30);
    return fetch(path, {
      ...opts,
      headers: { ...(opts.headers || {}), Authorization: "Bearer " + kc.token },
    });
  };

  const links = wireLinks(api);
  loadProfile(api);
  wireCreateForm(api, links.reload);
  wireStatsForm(api);
  wireBulk(api);
}

async function loadProfile(api) {
  try {
    const res = await api("/auth/me");
    if (!res.ok) return;
    const { data } = await res.json();
    text("greeting", `Signed in as ${data.username} (${data.email})`);
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
    const code = $("code").value.trim();
    if (!code) return;

    text("stats-result", "Loading…");
    let res, json;
    try {
      res = await api("/api/links/" + encodeURIComponent(code) + "/stats");
      json = await res.json().catch(() => ({}));
    } catch {
      text("stats-result", "Network error — please retry.");
      return;
    }

    if (res.ok) {
      text("stats-result", `Total clicks: ${json.data.total_clicks}`);
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

// wireLinks renders the user's paginated links and returns { reload }.
function wireLinks(api) {
  const PAGE = 20;
  let offset = 0;
  let total = 0;

  const render = (items) => {
    const body = $("links-body");
    body.textContent = "";
    for (const it of items) {
      const tr = document.createElement("tr");

      const short = document.createElement("td");
      short.append(linkAnchor(it.short_url), copyButton(it.short_url));
      tr.append(short);

      const original = document.createElement("td");
      original.className = "truncate";
      original.textContent = it.original_url;
      original.title = it.original_url;
      tr.append(original);

      for (const value of [it.total_clicks, new Date(it.created_at).toLocaleDateString(), expiryLabel(it.expires_at)]) {
        const td = document.createElement("td");
        td.textContent = value;
        tr.append(td);
      }
      body.append(tr);
    }
  };

  const load = async () => {
    text("links-status", "Loading…");
    let res, json;
    try {
      res = await api(`/api/links?limit=${PAGE}&offset=${offset}`);
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
      text("links-status", "No links yet.");
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
