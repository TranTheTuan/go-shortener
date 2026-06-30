// Minimal vanilla frontend for the Go URL Shortener.
//
// Authenticates against Keycloak (Authorization Code + PKCE via keycloak-js),
// then calls the same-origin JSON API with a Bearer token. Runtime config
// (auth URL / realm / client) is fetched from the backend so it never drifts.
import Keycloak from "/static/keycloak.js";

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

  loadProfile(api);
  wireCreateForm(api);
  wireStatsForm(api);
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

function wireCreateForm(api) {
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

    if (res.status === 201) {
      showCreated(json.data.short_url);
    } else if (res.status === 429) {
      text("result", "Daily quota exceeded — try again tomorrow.");
    } else {
      text("result", "Error: " + (json.error?.message || res.status));
    }
  };
}

function showCreated(shortURL) {
  const result = $("result");
  result.textContent = "";

  const link = document.createElement("a");
  link.href = shortURL;
  link.textContent = shortURL;
  link.target = "_blank";
  link.rel = "noopener";

  const copy = document.createElement("button");
  copy.textContent = "Copy";
  copy.onclick = async () => {
    await navigator.clipboard.writeText(shortURL);
    copy.textContent = "Copied!";
    setTimeout(() => (copy.textContent = "Copy"), 1500);
  };

  result.append(link, copy);
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

main();
