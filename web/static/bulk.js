// Bulk-upload UI: download template → pick CSV → presign → PUT to R2 → confirm
// → poll job status → list jobs with a result download link.
//
// The whole section stays hidden unless the backend actually exposes the
// /api/bulk-jobs routes (they're only registered when R2 is configured).
import { md5Base64 } from "/static/md5.js";

const $ = (id) => document.getElementById(id);
const text = (id, value) => {
  $(id).textContent = value; // textContent only — never innerHTML (XSS-safe)
};

const MAX_ROWS = 10000;
const POLL_MS = 3000;
const POLL_CAP = 40; // ~2 min, then give up polling (worker may be down)

let pollTimer; // single active poll — a new upload cancels the previous timer

// wireBulk attaches all bulk-upload handlers. `api` is the shared Bearer fetch.
export function wireBulk(api) {
  $("tpl-csv").onclick = () => downloadTemplate(api, "csv");
  $("tpl-xlsx").onclick = () => downloadTemplate(api, "xlsx");

  $("bulk-form").onsubmit = async (ev) => {
    ev.preventDefault();
    const file = $("bulk-file").files[0];
    if (file) await upload(api, file);
  };

  loadJobs(api); // also feature-detects: hides the section on 404
}

async function downloadTemplate(api, format) {
  try {
    const res = await api(`/api/bulk-jobs/template?format=${format}`);
    if (!res.ok) {
      text("bulk-status", "Template download failed.");
      return;
    }
    const href = URL.createObjectURL(await res.blob());
    const a = document.createElement("a");
    a.href = href;
    a.download = `template.${format}`;
    a.click();
    URL.revokeObjectURL(href);
  } catch {
    text("bulk-status", "Network error downloading template.");
  }
}

// csvRowCount = non-empty lines minus the header row.
function csvRowCount(buf) {
  const lines = new TextDecoder().decode(buf).split(/\r?\n/).filter((l) => l.trim() !== "");
  return Math.max(0, lines.length - 1);
}

async function upload(api, file) {
  const buf = await file.arrayBuffer();

  const rowCount = csvRowCount(buf);
  if (rowCount < 1 || rowCount > MAX_ROWS) {
    text("bulk-status", `CSV must have 1–${MAX_ROWS} data rows (found ${rowCount}).`);
    return;
  }
  const contentMD5 = md5Base64(buf);

  // 1. Presign.
  text("bulk-status", "Requesting upload URL…");
  const presign = await postJSON(api, "/api/bulk-jobs/upload-url", {
    filename: file.name,
    row_count: rowCount,
    content_md5: contentMD5,
  });
  if (!presign.ok) {
    text("bulk-status", "Error: " + presign.message);
    return;
  }
  if (!presign.data?.presigned_url) {
    text("bulk-status", "Unexpected server response.");
    return;
  }
  const { presigned_url, file_key } = presign.data;

  // 2. Cross-origin PUT straight to R2. No Bearer; the Content-MD5 header must
  //    match what was signed. Requires R2 bucket CORS (PUT + content-md5 + this
  //    origin) — otherwise the browser preflight fails.
  text("bulk-status", "Uploading to storage…");
  try {
    const put = await fetch(presigned_url, {
      method: "PUT",
      headers: { "Content-MD5": contentMD5 },
      body: buf,
    });
    if (!put.ok) {
      text("bulk-status", `Upload failed (${put.status}). Check R2 CORS / link expiry.`);
      return;
    }
  } catch {
    text("bulk-status", "Upload blocked — likely R2 CORS not configured.");
    return;
  }

  // 3. Confirm — registers the job and queues it.
  text("bulk-status", "Confirming…");
  const confirm = await postJSON(api, "/api/bulk-jobs", {
    file_key,
    filename: file.name,
    row_count: rowCount,
  });
  if (!confirm.ok) {
    text("bulk-status", "Error: " + confirm.message);
    return;
  }

  $("bulk-form").reset();
  text("bulk-status", `Job #${confirm.data.id} queued.`);
  await loadJobs(api);
  pollJob(api, confirm.data.id);
}

// postJSON posts a JSON body and normalizes the envelope into {ok,data,message}.
async function postJSON(api, path, body) {
  let res, json;
  try {
    res = await api(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    json = await res.json().catch(() => ({}));
  } catch {
    return { ok: false, message: "network error — please retry" };
  }
  return res.ok
    ? { ok: true, data: json.data }
    : { ok: false, message: json.error?.message || String(res.status) };
}

// pollJob refreshes the list until the job reaches a terminal state (cap ~2 min).
function pollJob(api, id) {
  clearInterval(pollTimer); // supersede any in-flight poll from a prior upload
  let ticks = 0;
  pollTimer = setInterval(async () => {
    ticks++;
    let job;
    try {
      const res = await api("/api/bulk-jobs/" + id);
      if (!res.ok) throw new Error();
      job = (await res.json()).data;
    } catch {
      if (ticks >= POLL_CAP) clearInterval(pollTimer);
      return; // transient — keep polling until the cap
    }
    text("bulk-status", `Job #${id}: ${job.status} (${job.done_rows}/${job.total_rows})`);
    await loadJobs(api);
    if (job.status === "completed" || job.status === "failed" || ticks >= POLL_CAP) {
      clearInterval(pollTimer);
    }
  }, POLL_MS);
}

function resultCell(job) {
  const td = document.createElement("td");
  if (job.status === "completed" && job.result_url) {
    const a = document.createElement("a");
    a.href = job.result_url;
    a.textContent = "Download";
    a.rel = "noopener";
    td.append(a);
  } else {
    td.textContent = "—";
  }
  return td;
}

async function loadJobs(api) {
  let res;
  try {
    res = await api("/api/bulk-jobs?limit=20&offset=0");
  } catch {
    return;
  }
  if (res.status === 404) {
    $("bulk").hidden = true; // R2 disabled server-side — hide the whole feature
    return;
  }
  $("bulk").hidden = false;

  const json = await res.json().catch(() => ({}));
  const jobs = json.data ?? [];
  if (!jobs.length) {
    text("bulk-jobs-status", "No bulk jobs yet.");
    $("bulk-jobs-table").hidden = true;
    return;
  }

  text("bulk-jobs-status", "");
  const body = $("bulk-jobs-body");
  body.textContent = "";
  for (const job of jobs) {
    const tr = document.createElement("tr");
    const cells = [job.id, job.status, `${job.done_rows}/${job.total_rows}`, new Date(job.created_at).toLocaleString()];
    for (const v of cells) {
      const td = document.createElement("td");
      td.textContent = v;
      tr.append(td);
    }
    tr.append(resultCell(job));
    body.append(tr);
  }
  $("bulk-jobs-table").hidden = false;
}
