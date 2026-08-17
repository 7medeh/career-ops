// Career Dashboard frontend. Talks to the Go backend via Wails bindings
// exposed on window.go.main.App.*  (each method returns a Promise).

const $ = (sel) => document.querySelector(sel);
const el = (tag, cls, text) => {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text != null) n.textContent = text;
  return n;
};

function backend() {
  return window.go && window.go.main && window.go.main.App;
}

function toast(msg) {
  const t = $("#toast");
  t.textContent = msg;
  t.hidden = false;
  clearTimeout(toast._timer);
  toast._timer = setTimeout(() => (t.hidden = true), 2600);
}

const STATUSES = ["Evaluated", "Applied", "Responded", "Interview", "Offer", "Rejected", "Discarded", "SKIP"];

// ---- Tabs ----
document.querySelectorAll(".tab").forEach((tab) => {
  tab.addEventListener("click", () => {
    document.querySelectorAll(".tab").forEach((t) => t.classList.remove("active"));
    document.querySelectorAll(".view").forEach((v) => v.classList.remove("active"));
    tab.classList.add("active");
    $("#" + tab.dataset.tab).classList.add("active");
  });
});

$("#refresh").addEventListener("click", () => loadAll());

// Sortable listings column headers.
document.querySelectorAll("#listings-table th[data-key]").forEach((th) => {
  th.addEventListener("click", () => onSortHeaderClick(th.dataset.key));
});

// Sortable pipeline column headers.
document.querySelectorAll("#apps-table th[data-key]").forEach((th) => {
  th.addEventListener("click", () => onPipelineSortClick(th.dataset.key));
});

// ---- Pipeline ----
function scoreClass(score) {
  if (score >= 4.2) return "hi";
  if (score >= 3.5) return "mid";
  return "lo";
}

// Column definitions for the pipeline table. Mirrors LISTING_COLUMNS: `type`
// picks the comparator and `defaultDir` is the direction on first click, so
// Score and # open high-to-low while text columns open A-Z.
const PIPELINE_COLUMNS = [
  { key: "Number", type: "num", defaultDir: "desc", value: (a) => a.Number || 0 },
  { key: "Company", type: "str", defaultDir: "asc", value: (a) => a.Company || "" },
  { key: "Role", type: "str", defaultDir: "asc", value: (a) => a.Role || "" },
  { key: "Score", type: "num", defaultDir: "desc", value: (a) => a.Score || 0 },
  { key: "Status", type: "str", defaultDir: "asc", value: (a) => a.Status || "" },
  { key: "CompEstimate", type: "num", defaultDir: "desc", value: (a) => salaryValue(a.CompEstimate) },
];

let appsData = [];
let appsMetrics = {};
let pipelineSort = { key: "Number", dir: "desc" };

function sortApps() {
  const col = PIPELINE_COLUMNS.find((c) => c.key === pipelineSort.key);
  if (!col) return;
  const dir = pipelineSort.dir === "asc" ? 1 : -1;
  appsData.sort((a, b) => {
    const av = col.value(a), bv = col.value(b);
    let cmp;
    if (col.type === "num") cmp = av - bv;
    else cmp = String(av).toLowerCase().localeCompare(String(bv).toLowerCase());
    if (cmp === 0) return (b.Number || 0) - (a.Number || 0); // stable tiebreak
    return cmp * dir;
  });
}

// Clicking a pipeline header sorts by it; clicking the active one flips it.
function onPipelineSortClick(key) {
  const col = PIPELINE_COLUMNS.find((c) => c.key === key);
  if (!col) return;
  if (pipelineSort.key === key) {
    pipelineSort.dir = pipelineSort.dir === "asc" ? "desc" : "asc";
  } else {
    pipelineSort = { key, dir: col.defaultDir };
  }
  sortApps();
  renderPipeline();
}

async function loadPipeline() {
  const app = backend();
  if (!app) return;
  const payload = await app.GetApplications();
  appsData = payload.apps || [];
  appsMetrics = payload.metrics || {};
  sortApps();
  renderPipeline();
}

function renderPipeline() {
  const apps = appsData;
  const m = appsMetrics;

  const metrics = $("#metrics");
  metrics.innerHTML = "";
  const cards = [
    ["Total", m.Total || 0],
    ["Avg score", (m.AvgScore || 0).toFixed(1)],
    ["Top score", (m.TopScore || 0).toFixed(1)],
    ["With PDF", m.WithPDF || 0],
    ["Actionable", m.Actionable || 0],
  ];
  for (const [lbl, num] of cards) {
    const card = el("div", "metric");
    card.appendChild(el("div", "num", String(num)));
    card.appendChild(el("div", "lbl", lbl));
    metrics.appendChild(card);
  }

  // Reflect sort direction in the header arrows.
  document.querySelectorAll("#apps-table th[data-key]").forEach((th) => {
    const arrow = th.querySelector(".arrow");
    if (!arrow) return;
    arrow.textContent = th.dataset.key === pipelineSort.key ? (pipelineSort.dir === "asc" ? "\u25B2" : "\u25BC") : "";
    th.classList.toggle("sorted", th.dataset.key === pipelineSort.key);
  });

  const body = $("#apps-body");
  body.innerHTML = "";
  $("#apps-empty").hidden = apps.length > 0;
  $("#apps-table").hidden = apps.length === 0;

  for (const a of apps) {
    const tr = el("tr");
    tr.appendChild(el("td", null, a.Number ? "#" + a.Number : "—"));
    tr.appendChild(el("td", "company", a.Company || "—"));
    tr.appendChild(el("td", "role", a.Role || "—"));

    const scoreTd = el("td", "score " + scoreClass(a.Score));
    scoreTd.textContent = a.Score ? a.Score.toFixed(1) : "—";
    tr.appendChild(scoreTd);

    const statusTd = el("td");
    const sel = el("select", "status-select");
    for (const s of STATUSES) {
      const opt = el("option", null, s);
      opt.value = s;
      if (a.Status && a.Status.toLowerCase() === s.toLowerCase()) opt.selected = true;
      sel.appendChild(opt);
    }
    sel.addEventListener("change", async () => {
      if (!a.ReportNumber) { toast("No report number to update."); return; }
      try {
        await backend().UpdateStatus(a.ReportNumber, sel.value);
        toast(`${a.Company} → ${sel.value}`);
      } catch (e) { toast("Update failed: " + e); }
    });
    statusTd.appendChild(sel);
    tr.appendChild(statusTd);

    tr.appendChild(el("td", null, a.CompEstimate || "—"));

    const actionTd = el("td");
    if (a.JobURL) {
      const open = el("button", "link-btn", "Open");
      open.addEventListener("click", () => backend().OpenURL(a.JobURL));
      actionTd.appendChild(open);
    }
    tr.appendChild(actionTd);

    body.appendChild(tr);
  }
}

// ---- Listings ----
function fitClass(fit) {
  if (fit >= 70) return "hi";
  if (fit >= 50) return "mid";
  return "lo";
}

// Parse the largest dollar figure in a salary string, for sorting.
function salaryValue(s) {
  if (!s) return 0;
  const k = /k/i.test(s);
  let max = 0;
  for (const m of s.matchAll(/(\d+(?:\.\d+)?)/g)) {
    let v = parseFloat(m[1]);
    if (k && v < 1000) v *= 1000;
    if (v > max) max = v;
  }
  return max;
}

// Column definitions: key = field, type drives the comparator, defaultDir is
// the direction on first click (so Fit/Salary/YOE start high-to-low).
const LISTING_COLUMNS = [
  { key: "Fit", type: "num", defaultDir: "desc", value: (l) => l.Fit || 0 },
  { key: "Company", type: "str", defaultDir: "asc", value: (l) => l.Company || "" },
  { key: "Title", type: "str", defaultDir: "asc", value: (l) => l.Title || "" },
  { key: "Salary", type: "num", defaultDir: "desc", value: (l) => salaryValue(l.Salary) },
  { key: "YOE", type: "num", defaultDir: "asc", value: (l) => l.YOE || 0 },
  { key: "FirstSeen", type: "str", defaultDir: "desc", value: (l) => l.FirstSeen || "" },
  { key: "Portal", type: "str", defaultDir: "asc", value: (l) => l.Portal || "" },
];

let listingsData = [];
let sortState = { key: "Fit", dir: "desc" };

function sortListings() {
  const col = LISTING_COLUMNS.find((c) => c.key === sortState.key);
  if (!col) return;
  const dir = sortState.dir === "asc" ? 1 : -1;
  listingsData.sort((a, b) => {
    const av = col.value(a), bv = col.value(b);
    let cmp;
    if (col.type === "num") cmp = av - bv;
    else cmp = String(av).toLowerCase().localeCompare(String(bv).toLowerCase());
    if (cmp === 0) return (b.Fit || 0) - (a.Fit || 0); // stable tiebreak by fit
    return cmp * dir;
  });
}

async function loadListings() {
  const app = backend();
  if (!app) return;
  listingsData = (await app.GetListings()) || [];
  sortListings();
  renderListings();
}

function renderListings() {
  $("#listings-count").textContent =
    listingsData.length + (listingsData.length === 1 ? " open role" : " open roles");

  // Reflect sort direction in the header arrows.
  document.querySelectorAll("#listings-table th[data-key]").forEach((th) => {
    const key = th.dataset.key;
    const arrow = th.querySelector(".arrow");
    if (!arrow) return;
    arrow.textContent = key === sortState.key ? (sortState.dir === "asc" ? "▲" : "▼") : "";
    th.classList.toggle("sorted", key === sortState.key);
  });

  const body = $("#listings-body");
  body.innerHTML = "";
  $("#listings-empty").hidden = listingsData.length > 0;
  $("#listings-table").hidden = listingsData.length === 0;

  for (const l of listingsData) {
    const tr = el("tr");

    const fitTd = el("td");
    if (l.Fit && l.Fit > 0) {
      const badge = el("span", "fit-badge " + fitClass(l.Fit), String(l.Fit));
      badge.title = "Heuristic fit (title, YOE, salary, location)";
      fitTd.appendChild(badge);
    } else {
      fitTd.textContent = "—";
    }
    tr.appendChild(fitTd);

    tr.appendChild(el("td", "company", l.Company || "—"));
    tr.appendChild(el("td", "title", l.Title || "—"));
    tr.appendChild(el("td", null, l.Salary || "—"));
    tr.appendChild(el("td", null, l.YOE && l.YOE > 0 ? l.YOE + "+ yrs" : "—"));
    tr.appendChild(el("td", null, l.FirstSeen || "—"));
    tr.appendChild(el("td", null, l.Portal || "—"));

    const actionTd = el("td");
    if (l.URL) {
      const open = el("button", "link-btn", "Open");
      open.addEventListener("click", () => backend().OpenURL(l.URL));
      actionTd.appendChild(open);
      const apply = el("button", "apply-btn", "Apply");
      apply.addEventListener("click", async () => {
        try {
          await backend().Apply(l.URL);
          toast("Opening an apply session in Terminal…");
        } catch (e) { toast("Could not start apply: " + e); }
      });
      actionTd.appendChild(apply);
    }
    tr.appendChild(actionTd);
    body.appendChild(tr);
  }
}

// Clicking a column header sorts by it; clicking the active column flips direction.
function onSortHeaderClick(key) {
  const col = LISTING_COLUMNS.find((c) => c.key === key);
  if (!col) return;
  if (sortState.key === key) {
    sortState.dir = sortState.dir === "asc" ? "desc" : "asc";
  } else {
    sortState = { key, dir: col.defaultDir };
  }
  sortListings();
  renderListings();
}

$("#scan").addEventListener("click", async () => {
  const btn = $("#scan");
  if (btn.disabled) return;
  const label = btn.textContent;
  btn.disabled = true;
  btn.textContent = "Scanning…";
  try {
    const r = await backend().Scan();
    await loadListings();
    let msg = `Scanned ${r.companiesScanned} companies · ${r.added} new`;
    if (r.enriched > 0) msg += ` · ${r.enriched} scored`;
    if (r.added === 0 && r.enriched === 0) msg += ` (nothing new; ${r.skippedDup} already known)`;
    if (r.errors && r.errors.length) msg += ` · ${r.errors.length} unreachable`;
    toast(msg);
  } catch (e) {
    toast("Scan failed: " + e);
  } finally {
    btn.disabled = false;
    btn.textContent = label;
  }
});

// Clear empties the listing stores so the next scan re-runs the CURRENT
// portals.yml filter over every posting, including ones an older filter
// rejected (scan-history.tsv is the scanner's dedup memory, and a rejected
// posting is suppressed permanently until that memory is cleared).
//
// Two-step inline confirm rather than confirm(): a modal dialog inside the
// webview blocks the Wails runtime, and this is destructive enough to deserve
// a deliberate second click.
let clearArmed = false;
const clearBtn = $("#clear");
const clearLabel = clearBtn.textContent;

function disarmClear() {
  clearArmed = false;
  clearBtn.textContent = clearLabel;
  clearBtn.classList.remove("armed");
}

clearBtn.addEventListener("click", async () => {
  if (clearBtn.disabled) return;

  if (!clearArmed) {
    clearArmed = true;
    clearBtn.textContent = "Click again to confirm";
    clearBtn.classList.add("armed");
    toast("Clears scanned listings and scan history. Your tracker is untouched, and a backup is written first.");
    setTimeout(disarmClear, 6000);
    return;
  }

  disarmClear();
  clearBtn.disabled = true;
  clearBtn.textContent = "Clearing\u2026";
  try {
    const r = await backend().ClearListings();
    await loadListings();
    toast(
      `Cleared ${r.scanHistoryRemoved} history rows and ${r.pipelineRemoved} inbox entries. ` +
      `Backup: ${r.backupDir}. Run a scan to repopulate.`
    );
  } catch (e) {
    toast("Clear failed: " + e);
  } finally {
    clearBtn.disabled = false;
    clearBtn.textContent = clearLabel;
  }
});

// ---- Preferences ----
let currentKeywords = [];
let existingKeywords = new Set();

function renderKeywords() {
  const row = $("#pref-keywords");
  row.innerHTML = "";
  for (const kw of currentKeywords) {
    row.appendChild(el("span", "chip", kw));
  }
}

async function loadPreferences() {
  const app = backend();
  if (!app) return;
  const p = await app.GetPreferences();
  $("#pref-target").value = p.SalaryTarget || "";
  $("#pref-min").value = p.SalaryMinimum || "";
  $("#pref-loc").value = p.LocationFlexibility || "";
  currentKeywords = (p.PositionKeywords || []).slice();
  existingKeywords = new Set(currentKeywords.map((k) => k.toLowerCase()));
  renderKeywords();
}

function addKeyword() {
  const input = $("#pref-newkw");
  const kw = input.value.trim();
  if (!kw) return;
  if (currentKeywords.some((k) => k.toLowerCase() === kw.toLowerCase())) {
    toast("Keyword already present.");
    input.value = "";
    return;
  }
  currentKeywords.push(kw);
  renderKeywords();
  input.value = "";
}

$("#pref-addkw").addEventListener("click", addKeyword);
$("#pref-newkw").addEventListener("keydown", (e) => {
  if (e.key === "Enter") { e.preventDefault(); addKeyword(); }
});

$("#prefs-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const status = $("#prefs-status");
  // Only newly-added keywords are written (backend appends, never removes).
  const newKeywords = currentKeywords.filter((k) => !existingKeywords.has(k.toLowerCase()));
  const prefs = {
    SalaryTarget: $("#pref-target").value.trim(),
    SalaryMinimum: $("#pref-min").value.trim(),
    LocationFlexibility: $("#pref-loc").value.trim(),
    PositionKeywords: newKeywords,
  };
  try {
    await backend().SavePreferences(prefs);
    status.textContent = "Saved";
    status.classList.remove("err");
    existingKeywords = new Set(currentKeywords.map((k) => k.toLowerCase()));
    setTimeout(() => (status.textContent = ""), 2000);
  } catch (err) {
    status.textContent = "Save failed: " + err;
    status.classList.add("err");
  }
});

// ---- Boot ----
async function loadAll() {
  try {
    await Promise.all([loadPipeline(), loadListings(), loadPreferences()]);
  } catch (e) {
    toast("Load error: " + e);
  }
}

function waitForBackend(attempt = 0) {
  if (backend()) { loadAll(); return; }
  if (attempt > 50) { toast("Backend not available."); return; }
  setTimeout(() => waitForBackend(attempt + 1), 100);
}

window.addEventListener("DOMContentLoaded", () => waitForBackend());
