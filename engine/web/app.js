"use strict";
const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const icon = n => `<svg><use href="#i-${n}"/></svg>`;
const ACTIVE = ["downloading", "processing", "converting", "paused", "queued"];
const LIVE = ["downloading", "processing", "converting"];
const VIDEO = /\.(mkv|mp4|m4v|avi|mov|ts|m2ts|wmv|webm)$/i;
const AUDIO = /\.(flac|mp3|m4a|aac|ogg|opus|wav|alac|aiff?)$/i;

function bytes(b) {
  if (b == null) return "unknown size";
  const u = ["B", "KB", "MB", "GB", "TB"]; let i = 0;
  while (b >= 1000 && i < u.length - 1) { b /= 1000; i++; }
  return (i < 2 ? Math.round(b) : b.toFixed(b >= 100 ? 0 : 1)) + " " + u[i];
}
const mbps = bps => (bps / 1e6).toFixed(bps >= 1e7 ? 0 : 1) + " MB/s";
function dur(s) {
  if (s == null || !isFinite(s)) return "";
  s = Math.round(s);
  if (s >= 3600) return `${Math.floor(s / 3600)} h ${String(Math.floor(s % 3600 / 60)).padStart(2, "0")} min`;
  if (s >= 60) return `${Math.round(s / 60)} min`;
  return `${s} s`;
}
function ago(t) {
  const s = Date.now() / 1000 - t;
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.round(s / 60)} min ago`;
  if (s < 86400) return `${Math.round(s / 3600)} h ago`;
  if (s < 7 * 86400) return `${Math.round(s / 86400)} d ago`;
  return new Date(t * 1000).toLocaleDateString(undefined, { day: "numeric", month: "short" });
}
const plural = (n, one, many = one + "s") => `${n} ${n === 1 ? one : many}`;
const pad2 = n => String(Number(n) || 0).padStart(2, "0");
// "Oldboy (2003)" -> Oldboy in serif with the year set smaller in italics
function titleHTML(name, extra) {
  const m = !extra && String(name).match(/^(.*?)\s*\((\d{4})\)$/);
  if (m) [name, extra] = [m[1], m[2]];
  return `${esc(name)}${extra ? `<em>${esc(extra)}</em>` : ""}`;
}

async function api(path, body) {
  try {
    const r = await fetch(path, body === undefined ? {} :
      { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
    const j = await r.json().catch(() => ({ error: `HTTP ${r.status}` }));
    if (!r.ok && !j.error) j.error = `HTTP ${r.status}`;
    return j;
  } catch (e) {
    return { error: "mint is unreachable (Tailscale on?)" };
  }
}

let toastT;
function toast(msg, bad) {
  const t = $("#toast");
  t.textContent = msg; t.className = "show" + (bad ? " bad" : "");
  clearTimeout(toastT); toastT = setTimeout(() => t.className = "", bad ? 5000 : 2800);
}

// ---------- dialogs ----------
// ask({title, text, input, buttons:[{label, value, cls}]}) -> {value, input}
// cls: "dark" for the main action, "danger" for destructive ones.
function ask({ title, text = "", input = false, buttons }) {
  const d = $("#ask");
  $("#askTitle").textContent = title; $("#askText").textContent = text;
  const inp = $("#askInput"); inp.hidden = !input; inp.value = "";
  $("#askBtns").innerHTML = buttons.map(b => `<button value="${esc(b.value)}" class="pill ${b.cls || ""}">${esc(b.label)}</button>`).join("");
  d.returnValue = "";
  d.showModal();
  if (input) setTimeout(() => inp.focus(), 50);
  return new Promise(res => d.addEventListener("close", () => res({ value: d.returnValue, input: inp.value.trim() }), { once: true }));
}

// ---------- live state ----------
let S = null, online = false, tab = "active", lastDone = null;
const openJobs = new Set();

function setLive(on) { online = on; renderStatus(); }

let es, pollT;
function connect() {
  if (!window.EventSource) { poll(); return; }
  es?.close();
  es = new EventSource("/api/events");
  es.onmessage = e => { S = JSON.parse(e.data); online = true; render(); };
  es.onerror = () => setLive(false);
}
async function poll() {
  const r = await api("/api/status");
  if (!r.error) { S = r; online = true; render(); } else setLive(false);
  clearTimeout(pollT); pollT = setTimeout(poll, 3000);
}
// iOS freezes background pages; reconnect as soon as we're visible again.
document.addEventListener("visibilitychange", () => {
  if (document.visibilityState === "visible" && (!es || es.readyState === 2)) connect();
if (location.hash === "#library" || wantItem) $(".tabs [data-tab=library]").click();
addEventListener("hashchange", () => {
  if (!location.hash.startsWith("#item=")) return;
  const p = decodeURIComponent(location.hash.slice(6));
  if (DETAIL?.node.path === p && $("#detail").open) return;
  wantItem = p;
  tab === "library" ? loadLibrary() : $(".tabs [data-tab=library]").click();
});
});

// ---------- rendering ----------
// The little status next to the settings button: Idle, the live speed, or
// what mint is busy with.
function renderStatus() {
  const st = $("#status");
  let cls = "", txt = "Idle";
  if (!online) [cls, txt] = S ? ["off", "Offline"] : ["paused", "Connecting"];
  else if (!S.aria2_ok) [cls, txt] = ["warn", "Engine restarting"];
  else {
    const jobs = S.jobs, post = jobs.find(j => j.state === "processing" || j.state === "converting");
    if (jobs.some(j => j.state === "downloading")) {
      cls = "busy";
      txt = mbps(S.speed_bps) + (S.throttled ? " · Media Core playing" : "");
    } else if (post) {
      cls = "busy";
      txt = (post.state === "converting" ? "Converting" : post.phase || "Processing") + (post.progress ? ` ${post.progress}%` : "");
    } else if (jobs.some(j => j.state === "paused")) [cls, txt] = ["paused", "Paused"];
    else if (jobs.some(j => j.state === "queued")) [cls, txt] = ["paused", "Waiting"];
  }
  st.className = "status " + cls;
  $("span", st).textContent = txt;
  st.title = S ? (S.settings.limit_mbps ? `Limited to ${S.settings.limit_mbps} MB/s` : "No speed limit") +
    (S.throttled ? `, eased to 1 MB/s while Media Core plays` : "") : "";
}

function render() {
  if (!S) return;
  renderStatus();
  const b = $("#banner");
  const lowDisk = S.disk_total && S.disk_free < 20e9;
  b.hidden = S.aria2_ok && !lowDisk;
  b.textContent = !S.aria2_ok ? "The download engine (aria2) isn't answering. mint is restarting it; downloads resume on their own."
    : lowDisk ? `Only ${bytes(S.disk_free)} left on mint's disk. The Library tab shows what's taking the space.` : "";
  const act = S.jobs.filter(j => ACTIVE.includes(j.state));
  const hist = S.jobs.filter(j => !ACTIVE.includes(j.state));
  $("#nActive").textContent = act.length || "";
  $("#nHistory").textContent = hist.length || "";
  $("#clear").hidden = tab !== "history" || !hist.some(j => j.state === "done" || j.state === "cancelled");
  const nDone = hist.filter(j => j.state === "done").length;
  if (tab === "library" && lastDone !== null && nDone !== lastDone) loadLibrary(); // something new arrived
  lastDone = nDone;
  $("#jobs").hidden = tab === "library";
  $("#library").hidden = tab !== "library";
  if (tab === "library") return;
  const rank = j => LIVE.includes(j.state) ? 0 : j.state === "paused" ? 1 : 2;
  const list = tab === "active"
    ? act.sort((a, b) => rank(a) - rank(b) || (a.state === "queued" ? a.queue_pos - b.queue_pos : a.created - b.created))
    : hist.sort((a, b) => (b.finished || b.updated) - (a.finished || a.updated));
  const box = $("#jobs");
  if (!list.length) {
    box.innerHTML = tab === "active"
      ? `<div class="empty"><b>Nothing downloading</b>Paste a link above and it lands in the library, named and sorted.</div>`
      : `<div class="empty"><b>No history yet</b>Finished downloads show up here.</div>`;
    return;
  }
  $(".empty", box)?.remove();
  const keep = new Set(list.map(j => j.id));
  for (const el of $$(".job", box)) if (!keep.has(el.dataset.id)) el.remove();
  let prev = null;
  for (const j of list) {
    let el = box.querySelector(`.job[data-id="${j.id}"]`);
    const html = jobHTML(j);
    if (!el) { el = document.createElement("article"); el.dataset.id = j.id; }
    if (el._html !== html) { el.innerHTML = html; el._html = html; }
    el.className = `item job st-${j.state}`;
    const want = prev ? prev.nextSibling : box.firstChild;
    if (el !== want) box.insertBefore(el, want);
    prev = el;
    const log = $("pre.log", el);
    if (log && !log._scrolled) { log.scrollTop = log.scrollHeight; log._scrolled = true; }
  }
}

// What a download will be called in the library: "Oldboy 2003", "True Detective S01E03".
function planTitle(p, fallback) {
  p = p || {};
  if (p.kind === "movie" && p.title) return [p.title, p.year || ""];
  if (p.kind === "tv" && p.title) return [p.title, p.episode !== "" && p.episode != null
    ? `S${pad2(p.season || 1)}E${pad2(p.episode)}` : `Season ${Number(p.season) || 1}`];
  if (p.kind === "music") return [p.title || "Music", ""];
  return [fallback || "Download", ""];
}

const STATE = { downloading: "Downloading", processing: "Processing", converting: "Dolby Vision fix", queued: "Waiting",
  paused: "Paused", done: "Done", failed: "Failed", cancelled: "Cancelled" };

function jobHTML(j) {
  const total = j.total_bytes || j.size;
  const [name, extra] = planTitle(j.plan, (j.dest || "").split("/").pop() || j.filename);
  const sub = [`<span class="state">${esc(j.state === "processing" && j.phase ? j.phase : STATE[j.state] || j.state)}</span>`];
  let right = "", bar = "";
  if (j.state === "downloading" || j.state === "paused" || j.state === "queued") {
    bar = `<div class="bar"><i style="width:${j.progress || 0}%"></i></div>`;
    right = j.done_bytes ? `${j.progress}%` : bytes(total);
    if (j.done_bytes) sub.push(`<span>${bytes(j.done_bytes)} of ${bytes(total)}</span>`);
    if (j.state === "downloading") {
      if (j.speed_bps) sub.push(`<span>${mbps(j.speed_bps)}</span>`);
      if (j.eta_s) sub.push(`<span>${dur(j.eta_s)} left</span>`);
    } else if (j.state === "queued" && total) {
      const s = S.settings.avg_bps > 1e5 ? S.settings.avg_bps : 3.5e6;
      sub.push(`<span>about ${dur((total - (j.done_bytes || 0)) / s)}</span>`);
    }
  } else if (j.state === "processing" || j.state === "converting") {
    bar = `<div class="bar ${j.progress ? "" : "indet"}"><i style="width:${j.progress || 0}%"></i></div>`;
    if (j.state === "converting" && j.phase) sub.push(`<span>${esc(j.phase.replace(/^Dolby Vision 7 → 8\.1: /, ""))}</span>`);
    right = j.progress ? `${j.progress}%` : "";
  } else {
    right = bytes(total);
    if (j.finished || j.updated) sub.push(`<span>${ago(j.finished || j.updated)}</span>`);
  }
  const b = (act, ic, label, cls = "") => `<button class="icon ${cls}" data-act="${act}" title="${label}" aria-label="${label}">${icon(ic)}</button>`;
  const acts = [];
  if (j.state === "downloading") acts.push(b("pause", "pause", "Pause"));
  if (j.state === "queued") acts.push(b("top", "top", "Download next"), b("pause", "pause", "Pause"));
  if (j.state === "paused") acts.push(b("resume", "play", "Resume"));
  if (["failed", "cancelled"].includes(j.state)) acts.push(b("retry", "retry", "Try again"), b("newlink", "link", "Use a fresh link"));
  if (ACTIVE.includes(j.state)) acts.push(b("cancel", "x", "Cancel"));
  else acts.push(b("remove", "x", "Remove from history"));
  const msg = ["failed", "cancelled"].includes(j.state) && j.message ? `<div class="sub msg">${esc(j.message)}</div>` : "";
  const more = openJobs.has(j.id) ? `<div class="more">
      <div class="kv"><b>file</b> ${esc(j.filename)}</div>
      ${j.dest ? `<div class="kv"><b>into</b> ${esc(j.dest)}</div>` : ""}
      <div class="kv"><b>from</b> ${esc(j.url)}</div>
      <pre class="log">${esc((j.log || []).join("\n"))}</pre></div>` : "";
  return `<div class="main"><div class="title">${titleHTML(name, extra)}</div><div class="sub">${sub.join("")}</div></div>
    ${right ? `<div class="size">${right}</div>` : ""}<div class="acts">${acts.join("")}</div>${msg}${more}${bar}`;
}

$("#jobs").addEventListener("click", async e => {
  const row = e.target.closest(".job"); if (!row || e.target.closest(".more")) return;
  const id = row.dataset.id;
  const btn = e.target.closest("button[data-act]");
  if (!btn) { openJobs.has(id) ? openJobs.delete(id) : openJobs.add(id); render(); return; } // tap a row for details
  const j = S.jobs.find(x => x.id === id);
  let act = btn.dataset.act, body = {};
  if (act === "cancel") {
    const r = await ask({ title: "Cancel this download?", text: j.state === "processing" || j.state === "converting"
      ? "Stops processing. Try again later picks it up where it left off."
      : "Keep the partial file to resume later, or delete it.", buttons: [
      { label: "Cancel, keep partial file", value: "keep", cls: "danger" },
      ...(LIVE.includes(j.state) && j.state !== "downloading" ? [] : [{ label: "Cancel and delete partial file", value: "delete", cls: "danger" }]),
      { label: "Keep going", value: "" }] });
    if (!r.value) return;
    body = { delete: r.value === "delete" };
  }
  if (act === "newlink") {
    const r = await ask({ title: "A fresh link for the same file", text: "It resumes where it stopped. The size must match.",
      input: true, buttons: [{ label: "Resume with this link", value: "ok", cls: "dark" }, { label: "Back", value: "" }] });
    if (!r.value || !r.input) return;
    act = "retry"; body = { url: r.input };
    toast("Checking the new link…");
  }
  if (act === "remove") row.style.opacity = .4;
  btn.disabled = true;
  const r = await api(`/api/jobs/${id}/${act}`, body);
  btn.disabled = false;
  if (r.error) toast(r.error, true);
  if (!es || es.readyState !== 1) poll();
});

for (const t of $$(".tabs [role=tab]")) t.onclick = () => {
  tab = t.dataset.tab;
  if (!location.hash.startsWith("#item=")) history.replaceState(null, "", tab === "library" ? "#library" : location.pathname + location.search);
  $$(".tabs [role=tab]").forEach(x => x.classList.toggle("on", x === t));
  if (tab === "library") loadLibrary();
  render();
};
$("#clear").onclick = async () => { const r = await api("/api/clear", {}); toast(`Cleared ${r.removed || 0}`); };

// ---------- adding links ----------
const ta = $("#links"), drop = $("#drop");
const urlsIn = text => [...new Set((text.match(/https?:\/\/[^\s<>"'`]+/g) || []).map(u => u.replace(/[.,;)\]>'"]+$/, "")))];
function grow() {
  ta.style.height = "auto"; ta.style.height = ta.scrollHeight + "px";
  drop.classList.toggle("multi", ta.scrollHeight > 60);
}
ta.addEventListener("input", grow);
ta.addEventListener("keydown", e => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); check(); } });
ta.addEventListener("paste", () => setTimeout(() => { grow(); if (urlsIn(ta.value).length) check(); }, 0));
drop.addEventListener("submit", e => { e.preventDefault(); if (ta.value.trim()) check(); else ta.focus(); });
drop.addEventListener("dragover", e => { e.preventDefault(); drop.classList.add("drag"); });
drop.addEventListener("dragleave", () => drop.classList.remove("drag"));
drop.addEventListener("drop", e => {
  e.preventDefault(); drop.classList.remove("drag");
  ta.value = e.dataTransfer.getData("text/uri-list") || e.dataTransfer.getData("text/plain"); grow(); check();
});

async function check() {
  const urls = urlsIn(ta.value);
  if (!urls.length) { if (ta.value.trim()) toast("No http(s) link in there", true); return; }
  ta.value = ""; grow(); ta.blur();
  const box = $("#probes");
  const cards = urls.map(u => {
    const el = document.createElement("div");
    el.className = "probe";
    el.innerHTML = loadingHTML(u);
    box.append(el);
    return [u, el];
  });
  updateAllBar();
  let i = 0;
  const worker = async () => { while (i < cards.length) { const [u, el] = cards[i++]; await probeInto(u, el); } };
  await Promise.all([worker(), worker(), worker()]);
}
const loadingHTML = u => `<div class="loading"><span class="spin"></span>Checking ${esc(u.length > 90 ? u.slice(0, 90) + "…" : u)}</div>`;

async function probeInto(url, el) {
  const p = await api("/api/probe", { url });
  if (!p.ok) {
    el.innerHTML = `<div class="ptitle">That link didn't work</div><div class="pfile">${esc(url)}</div>
      <div class="warn err">${esc(p.error)}</div>
      <div class="pacts" style="margin-top:16px"><button class="text" data-x>Dismiss</button><button class="pill" data-again>Try again</button></div>`;
    $("[data-x]", el).onclick = () => { el.remove(); updateAllBar(); };
    $("[data-again]", el).onclick = () => { el.innerHTML = loadingHTML(url); probeInto(url, el); };
    updateAllBar();
    return;
  }
  el._probe = p;
  const pl = p.plan;
  const facts = [`<span class="mono">${bytes(p.size)}</span>`,
    `<span class="${p.resumable ? "good" : "bad"}">${p.resumable ? "resumes" : "can't resume"}</span>`,
    `<span>${plural(p.connections, "connection")}</span>`];
  if (p.eta_min != null) facts.push(`<span>about ${dur(p.eta_min * 60)}</span>`);
  facts.push(`<span>${esc(p.host)}</span>`);
  el.innerHTML = `
    <div class="ptitle" data-title></div>
    <div class="pfile">${esc(p.filename)}</div>
    <div class="facts">${facts.join("")}</div>
    ${p.warnings.map(w => `<div class="warn">${esc(w)}</div>`).join("")}
    <div class="form">
      <div class="pills" data-kind>
        <button type="button" data-v="movie">Film</button><button type="button" data-v="tv">Show</button><button type="button" data-v="music">Music</button></div>
      <label class="c4" data-f="title"><span></span><input name="title" autocomplete="off"></label>
      <label class="c2" data-f="year">Year<input name="year" inputmode="numeric" autocomplete="off"></label>
      <label class="c3" data-f="season">Season<input name="season" inputmode="numeric" autocomplete="off"></label>
      <label class="c3" data-f="episode">Episode <small>(blank for a whole season)</small><input name="episode" inputmode="numeric" autocomplete="off"></label>
      <div class="dest" data-dest>…</div>
    </div>
    <div class="pacts"><button class="text" data-x>Dismiss</button><button class="pill dark" data-go>Download</button></div>`;
  for (const k of ["title", "year", "season", "episode"]) {
    const inp = $(`input[name=${k}]`, el);
    inp.value = pl[k] ?? "";
    if (k === "title" && pl.kind === "music") inp.value = "";
    inp.oninput = () => refreshPlan(el);
  }
  el._kind = pl.kind;
  for (const sb of $$("[data-kind] button", el)) sb.onclick = () => {
    if (sb.dataset.v === "music" && el._kind !== "music") $("input[name=title]", el).value = "";
    el._kind = sb.dataset.v; refreshPlan(el);
  };
  $("[data-x]", el).onclick = () => { el.remove(); updateAllBar(); };
  $("[data-go]", el).onclick = () => queue(el);
  refreshPlan(el);
  updateAllBar();
}

function planOf(el) {
  const v = n => $(`input[name=${n}]`, el).value.trim();
  return { ...el._probe.plan, kind: el._kind, title: v("title"), year: v("year"), season: v("season"), episode: v("episode") };
}

function refreshPlan(el) {
  const k = el._kind;
  for (const sb of $$("[data-kind] button", el)) sb.classList.toggle("on", sb.dataset.v === k);
  $("[data-f=year]", el).hidden = k !== "movie";
  $("[data-f=season]", el).hidden = $("[data-f=episode]", el).hidden = k !== "tv";
  $("[data-f=title]", el).className = k === "movie" ? "c4" : "c6";
  $("[data-f=title] span", el).textContent = k === "music" ? "Artist (blank to read it from the tags)" : k === "tv" ? "Show" : "Title";
  $("[data-title]", el).innerHTML = titleHTML(...planTitle(planOf(el), el._probe.filename));
  clearTimeout(el._t);
  el._t = setTimeout(async () => {
    const r = await api("/api/plan", { plan: planOf(el) });
    $("[data-dest]", el).innerHTML = r.error ? `<span class="err">${esc(r.error)}</span>`
      : `${esc(r.dest)}${r.note ? ` <small>(${esc(r.note)})</small>` : ""}`;
  }, 200);
}

async function queue(el) {
  const go = $("[data-go]", el);
  go.disabled = true; go.textContent = "Queuing…";
  const r = await api("/api/jobs", { probe: el._probe, plan: planOf(el) });
  if (r.error) { toast(r.error, true); go.disabled = false; go.textContent = "Download"; return false; }
  el.remove(); updateAllBar();
  toast("Queued: " + (r.dest || planOf(el).title || el._probe.filename));
  if (tab !== "active") $(".tabs [data-tab=active]").click();
  return true;
}

function updateAllBar() {
  const box = $("#probes");
  $(".allbar", box)?.remove();
  const ready = $$(".probe", box).filter(el => el._probe);
  if (ready.length < 2) return;
  const bar = document.createElement("div");
  bar.className = "allbar";
  bar.innerHTML = `<button class="pill dark">Download all ${ready.length}</button>`;
  $("button", bar).onclick = async () => { for (const el of ready) if (!(await queue(el))) break; };
  box.append(bar);
}

// ---------- library ----------
// Everything on mint's disk, dressed with the catalog's (TMDB) artwork and details.
// Rows (or a poster grid) open a detail sheet; anything can be deleted
// from the film/show/album down to a single episode, track or subtitle.
let wantItem = location.hash.startsWith("#item=") ? decodeURIComponent(location.hash.slice(6)) : "";
let LIB = null, libSec = "Movies", libView = localStorage.libView || "list", ITEMS = [], DETAIL = null;

async function loadLibrary() {
  const r = await api("/api/library");
  if (r.error) { if (!LIB) $("#libList").innerHTML = `<div class="empty err">${esc(r.error)}</div>`; return; }
  LIB = r; renderLibrary();
  if (wantItem) { // deep link: #item=Movies/Oldboy (2003)
    for (const sec of ["Movies", "TV Shows", "Music"]) {
      if (!wantItem.startsWith(sec + "/")) continue;
      $(`#libSec [data-v="${sec}"]`).click();
      const it = libItems().find(x => x.node.path === wantItem);
      if (it) openDetail(it);
    }
    wantItem = "";
  }
  if (DETAIL && $("#detail").open) { // refresh an open sheet in place, or close it if its item is gone
    const it = libItems().find(x => x.node.path === DETAIL.node.path);
    it ? openDetail(it, true) : $("#detail").close();
  }
}

const count = (n, re) => n.dir ? (n.children || []).reduce((a, c) => a + count(c, re), 0) : re.test(n.name) ? 1 : 0;
const files = n => n.dir ? (n.children || []).flatMap(files) : [n];
const mainVideo = n => files(n).filter(f => VIDEO.test(f.name)).sort((a, b) => b.size - a.size)[0];
const art = u => u; // catalog artwork URLs are already sized (/api/v1/img/<size>/...)
function runtime(ms) {
  const m = Math.round(ms / 60000);
  return m >= 60 ? `${Math.floor(m / 60)} h ${m % 60} min` : `${m} min`;
}

// The things the Library lists: films, shows, or (for music) albums.
function libItems() {
  const sec = LIB?.[libSec]; if (!sec) return [];
  const out = [];
  const name = n => n.dir ? n.name : n.name.replace(/\.[^.]+$/, "");
  for (const n of sec.children || []) {
    if (libSec === "Music") {
      const albums = (n.children || []).filter(c => c.dir);
      for (const a of albums) out.push({ node: a, kind: "album", meta: a.meta, artist: n.meta?.title || n.name,
        title: a.meta?.title || a.name.replace(/\s*\((Remaster|Deluxe)[^)]*\)$/i, ""), year: a.meta?.year, info: a.info });
      if (!albums.length || n.children.some(c => !c.dir && AUDIO.test(c.name)))
        out.push({ node: n, kind: "album", meta: n.meta, artist: n.meta?.title || n.name, title: albums.length ? "Singles" : name(n), loose: !!albums.length,
            info: n.info });
      continue;
    }
    const m = (name(n)).match(/^(.*?)\s*\((\d{4})\)$/);
    const main = libSec === "Movies" ? (n.dir ? mainVideo(n) : n) : null;
    out.push({ node: n, kind: libSec === "Movies" ? "film" : "show", meta: n.meta,
      title: n.meta?.title || (m ? m[1] : name(n)), year: n.meta?.year || (m ? m[2] : ""), main, info: main?.info });
  }
  return out;
}

// ---- format badges: small marks instead of "Dolby Vision 8.1 · DTS-HD MA" text
const DD = `<svg class="dd" viewBox="0 0 25 16" aria-hidden="true"><path d="M0 0h3a8 8 0 0 1 0 16H0zM25 0h-3a8 8 0 0 0 0 16h3z"/></svg>`;
const dolby = (word, title) => `<span class="bdg dolby" title="${title}">${DD}<b>${word}</b></span>`;
const box = (t, title = t) => `<span class="bdg box" title="${title}">${t}</span>`;
function badges(i, kind) {
  if (!i) return "";
  const b = [];
  if (kind === "album") {
    const lossless = /^(FLAC|ALAC|PCM)/.test(i.audio || "");
    if (i.bits > 16 || i.rate > 48000) b.push(box(`HI-RES`, "High resolution audio"));
    if (i.audio) b.push(box(i.audio.split(" ")[0]));
    if (lossless && i.bits && i.rate) b.push(`<span class="bdg q">${i.bits}/${+(i.rate / 1000).toFixed(1)}</span>`);
    return b.join("");
  }
  if (i.res) b.push(box(i.res === "4K" ? "4K" : i.res.replace(/p$/, "P"), i.res));
  const h = i.hdr || "";
  if (h.startsWith("Dolby Vision")) b.push(dolby("VISION", h));
  else if (h) b.push(box(h));
  const a = i.audio || "";
  if (/Atmos/.test(a)) b.push(dolby("ATMOS", a));
  else if (/^DTS/.test(a)) b.push(`<span class="bdg dts" title="${a}"><i>dts</i>${/HD MA/.test(a) ? "<b>HD MA</b>" : /:X/.test(a) ? "<b>X</b>" : ""}</span>`);
  else if (/^TrueHD/.test(a)) b.push(dolby("TRUEHD", a));
  else if (/^DD\+/.test(a)) b.push(dolby("DIGITAL+", a));
  else if (/^Dolby Digital/.test(a)) b.push(dolby("DIGITAL", a));
  return b.join("");
}

function poster(it, w, h) {
  const t = it.meta?.thumb;
  const src = t ? art(t, w, h) : it.node.cover ? `/api/cover?p=${encodeURIComponent(it.node.cover)}` : "";
  const m = it.meta, prog = m && m.offset && m.duration ? m.offset / m.duration : 0;
  return `<div class="poster ${it.kind === "album" ? "sq" : ""}">${src ? `<img src="${src}" alt="" loading="lazy" decoding="async">`
    : `<span class="ph">${esc(it.title)}</span>`}${prog ? `<i class="prog" style="width:${(prog * 100).toFixed(0)}%"></i>` : ""}</div>`;
}

// watched state, tucked away: "watched", "half way", "3 of 8 watched"
function watched(it) {
  const m = it.meta; if (!m) return "";
  if (it.kind === "show") return m.leaves ? (m.seen === m.leaves ? "watched" : m.seen ? `${m.seen} of ${m.leaves} watched` : "") : "";
  if (it.kind === "film") return m.viewed ? "watched" : ""; // part-watched shows as a bar on the poster
  return "";
}

function subline(it) {
  const m = it.meta || {}, n = it.node, s = [];
  if (it.kind === "film" && m.duration) s.push(runtime(m.duration));
  if (it.kind === "show") {
    const seasons = (n.children || []).filter(c => c.dir).length, eps = count(n, VIDEO);
    if (seasons) s.push(plural(seasons, "season")); if (eps) s.push(plural(eps, "episode"));
  }
  if (it.kind === "album") { s.push(it.artist); if (it.year) s.push(it.year); s.push(plural(count(n, AUDIO), "track")); }
  const w = watched(it); if (w) s.push(w);
  if (n.added && it.kind !== "album") s.push("added " + ago(n.added));
  return s.map(x => `<span>${esc(x)}</span>`).join("");
}

function renderLibrary() {
  if (!LIB) { $("#libList").innerHTML = `<div class="empty"><span class="spin"></span></div>`; return; }
  const sec = LIB[libSec] || { children: [] };
  const q = $("#libSearch").value.trim().toLowerCase();
  const sort = $("#libSort").value;
  const all = libItems();
  const hay = it => [it.title, it.artist, it.node.name, ...(it.meta?.cast || []), ...(it.meta?.genres || [])].join(" ").toLowerCase();
  const seenRank = it => it.meta?.viewed || (it.meta?.leaves && it.meta.seen === it.meta.leaves) ? 0 : 1;
  ITEMS = all.filter(it => !q || hay(it).includes(q)).sort((a, b) =>
    sort === "size" ? b.node.size - a.node.size : sort === "name" ? a.title.localeCompare(b.title)
      : sort === "watched" ? seenRank(a) - seenRank(b) || b.node.size - a.node.size : b.node.added - a.node.added);
  const noun = { Movies: "film", "TV Shows": "show", Music: "album" }[libSec];
  $("#libSum").innerHTML = `${plural(all.length, noun)} · <span class="mono">${bytes(sec.size || 0)}</span>`;
  const used = LIB.disk_total ? 1 - LIB.disk_free / LIB.disk_total : 0;
  $("#libDisk").innerHTML = `<span><span class="mono">${bytes(LIB.disk_free)}</span> free</span>
    <span class="meter ${LIB.disk_free < 20e9 ? "low" : ""}" title="${Math.round(used * 100)}% of ${bytes(LIB.disk_total)} used"><i style="width:${(used * 100).toFixed(1)}%"></i></span>`;
  const list = $("#libList");
  list.className = libView === "grid" ? "grid" + (libSec === "Music" ? " sqgrid" : "") : "rows";
  if (!ITEMS.length) {
    list.innerHTML = q ? `<div class="empty"><b>No matches</b>Nothing here matches “${esc(q)}”.</div>`
      : `<div class="empty"><b>Nothing here yet</b>Downloads land here once they're done.</div>`;
    return;
  }
  list.innerHTML = ITEMS.map((it, i) => libView === "grid"
    ? `<button class="tile" data-i="${i}">${poster(it, 320, it.kind === "album" ? 320 : 480)}
        <span class="tt">${esc(it.title)}${it.year && it.kind !== "album" ? ` <em>${esc(it.year)}</em>` : ""}</span>
        <span class="ts">${it.kind === "album" ? esc(it.artist) : ""}</span><span class="bdgs">${badges(it.info, it.kind)}</span></button>`
    : `<div class="item lib" data-i="${i}">${poster(it, 104, it.kind === "album" ? 104 : 156)}
        <div class="main"><div class="tline"><span class="title">${esc(it.title)}${it.year && it.kind !== "album" ? `<em>${esc(it.year)}</em>` : ""}</span>
          <span class="bdgs">${badges(it.info, it.kind)}</span></div><div class="sub">${subline(it)}</div></div>
        <div class="size">${bytes(it.node.size)}</div><div class="acts">${delBtn(it.node, it.title)}</div></div>`).join("");
}

const delBtn = (n, label = n.name) => `<button class="icon del" data-del="${esc(n.path)}" data-name="${esc(label)}" data-size="${n.size}"
  data-files="${n.files}" title="Delete" aria-label="Delete ${esc(label)}">${icon("trash")}</button>`;

async function deleteFromLibrary(b) {
  const { del: path, name } = b.dataset, size = Number(b.dataset.size), n = Number(b.dataset.files);
  const r = await ask({ title: `Delete “${name}”?`,
    text: `This permanently removes ${bytes(size)} (${plural(n, "file")}) from mint's disk, ` +
      `There's no undo.`,
    buttons: [{ label: `Delete ${bytes(size)}`, value: "del", cls: "danger" }, { label: "Keep it", value: "" }] });
  if (r.value !== "del") return;
  b.disabled = true;
  const res = await api("/api/library/delete", { path });
  if (res.error) { b.disabled = false; toast(res.error, true); return; }
  toast(`Deleted ${name} · ${bytes(res.freed)} freed`);
  loadLibrary();
}

$("#libList").addEventListener("click", e => {
  const b = e.target.closest("[data-del]");
  if (b) { e.stopPropagation(); deleteFromLibrary(b); return; }
  const row = e.target.closest("[data-i]");
  if (row) openDetail(ITEMS[row.dataset.i]);
});

// ---- detail sheet
const LANGS = { en: "English", eng: "English", hi: "Hindi", hin: "Hindi", ko: "Korean", kor: "Korean", es: "Spanish", spa: "Spanish",
  fr: "French", fre: "French", fra: "French", de: "German", ger: "German", deu: "German", ja: "Japanese", jpn: "Japanese",
  it: "Italian", ita: "Italian", pt: "Portuguese", por: "Portuguese", zh: "Chinese", chi: "Chinese", zho: "Chinese",
  ar: "Arabic", ara: "Arabic", ru: "Russian", rus: "Russian", ta: "Tamil", tam: "Tamil", te: "Telugu", tel: "Telugu", ur: "Urdu", urd: "Urdu" };

function fileLabel(n) {
  if (/\.(srt|ass|ssa|sub|idx|vtt)$/i.test(n.name)) {
    const tags = n.name.toLowerCase().split(".").slice(1, -1);
    return ["Subtitles", tags.map(t => LANGS[t]).find(Boolean), tags.includes("forced") ? "forced" : "", tags.includes("sdh") ? "SDH" : ""].filter(Boolean).join(" · ");
  }
  return n.name;
}

function heroStyle(m) {
  const c = m?.colors;
  if (!c) return "";
  const [tl, tr, br, bl] = c.map(x => "#" + x);
  return `background: radial-gradient(at 0 0, ${tl}, transparent 70%), radial-gradient(at 100% 0, ${tr}, transparent 70%),
    radial-gradient(at 100% 100%, ${br}, transparent 70%), radial-gradient(at 0 100%, ${bl}, transparent 70%), ${bl};`;
}

function techRows(i) {
  if (!i) return "";
  const rows = [];
  if (i.vcodec || i.res) rows.push(["Picture", [i.res, i.vcodec, i.hdr, i.mbps ? `${i.mbps} Mb/s` : ""].filter(Boolean).join(" · ")]);
  if (i.tracks?.length) rows.push(["Sound", esc(i.tracks[0]) + (i.tracks.length > 1
    ? `<details class="more2"><summary>+${i.tracks.length - 1} more</summary>${i.tracks.slice(1).map(esc).join("<br>")}</details>` : "")]);
  else if (i.audio) rows.push(["Sound", esc([i.audio, i.bits && `${i.bits}-bit`, i.rate && `${+(i.rate / 1000).toFixed(1)} kHz`].filter(Boolean).join(" · "))]);
  if (i.subs?.length) rows.push(["Subtitles", i.subs.length > 4
    ? `${esc(i.subs.slice(0, 3).join(", "))}<details class="more2"><summary>+${i.subs.length - 3} more</summary>${esc(i.subs.slice(3).join(", "))}</details>` : esc(i.subs.join(", "))]);
  return rows.map(([k, v]) => `<div class="kvrow"><span>${k}</span><div>${k === "Picture" ? esc(v) : v}</div></div>`).join("");
}

function fileRow(n, label, extra = "") {
  return `<div class="frow"><span class="fname">${label}</span>${extra}<span class="size">${bytes(n.size)}</span>${delBtn(n, n.dir ? label : n.name)}</div>`;
}

function openDetail(it, refresh) {
  DETAIL = it;
  const m = it.meta || {}, n = it.node, d = $("#detail");
  const kicker = [it.kind === "film" ? "Film" : it.kind === "show" ? "Series" : it.loose ? "Singles" : "Album",
    it.kind === "album" ? "" : it.year, m.rated, it.kind === "film" && m.duration ? runtime(m.duration) : "",
    m.rating ? `★ ${m.rating.toFixed(1)}` : ""].filter(Boolean);
  let body = "";
  if (m.summary) body += `<p class="summary ${m.summary.length > 320 ? "clamp" : ""}">${esc(m.summary)}</p>`;
  const facts = [m.genres?.length && ["Genre", m.genres.join(", ")], m.cast?.length && ["Starring", m.cast.join(", ")],
    m.studio && [it.kind === "album" ? "Label" : "Studio", m.studio]].filter(Boolean);
  if (it.kind === "film") {
    body += `<div class="kv">${techRows(it.info)}${facts.map(([k, v]) => `<div class="kvrow"><span>${k}</span><div>${esc(v)}</div></div>`).join("")}</div>`;
    const extras = files(n).filter(f => f !== it.main);
    body += `<h4>Files</h4><div class="flist">${it.main ? fileRow(it.main, "Film", `<span class="fdet">${esc(it.main.name)}</span>`) : ""}${extras.map(f => fileRow(f, esc(fileLabel(f)))).join("")}</div>`;
  } else if (it.kind === "show") {
    body += `<div class="kv">${facts.map(([k, v]) => `<div class="kvrow"><span>${k}</span><div>${esc(v)}</div></div>`).join("")}</div>`;
    const seasons = (n.children || []).filter(c => c.dir), loose = (n.children || []).filter(c => !c.dir);
    for (const s of seasons) {
      const eps = files(s);
      body += `<div class="season"><h4>${esc(s.meta?.title || s.name.replace(/^Season 0*(\d+)$/i, "Season $1"))}
        <span class="h4m">${plural(count(s, VIDEO), "episode")} · <span class="mono">${bytes(s.size)}</span></span>${delBtn(s, s.name)}</h4>
        <div class="flist">${eps.map(f => {
          const em = f.meta, num = em?.index ?? (f.name.match(/E(\d+)/i) || [])[1];
          const lbl = VIDEO.test(f.name) ? `<span class="epn">${num ? Number(num) : "·"}</span>${esc(em?.title || f.name.replace(/\.[^.]+$/, ""))}${em?.viewed ? `<span class="seen" title="Watched">✓</span>` : ""}`
            : esc(fileLabel(f));
          return fileRow(f, lbl, VIDEO.test(f.name) && f.info ? `<span class="fdet">${esc([f.info.res, f.info.hdr].filter(Boolean).join(" · "))}</span>` : "");
        }).join("")}</div></div>`;
    }
    if (loose.length) body += `<h4>Other files</h4><div class="flist">${loose.map(f => fileRow(f, esc(fileLabel(f)))).join("")}</div>`;
  } else {
    body += `<div class="kv">${techRows(it.info)}${facts.map(([k, v]) => `<div class="kvrow"><span>${k}</span><div>${esc(v)}</div></div>`).join("")}</div>`;
    const tracks = (it.loose ? (n.children || []).filter(c => !c.dir) : files(n)).filter(f => AUDIO.test(f.name));
    const other = (it.loose ? [] : files(n)).filter(f => !AUDIO.test(f.name));
    body += `<h4>Tracks</h4><div class="flist">${tracks.map(f => {
      const tm = f.meta, num = tm?.index ?? (f.name.match(/^(\d+)/) || [])[1];
      return fileRow(f, `<span class="epn">${num ? Number(num) : "·"}</span>${esc(tm?.title || f.name.replace(/\.[^.]+$/, "").replace(/^\d+\s*[-.]\s*/, ""))}`,
        tm?.duration ? `<span class="fdet mono">${Math.floor(tm.duration / 60000)}:${String(Math.round(tm.duration / 1000) % 60).padStart(2, "0")}</span>` : "");
    }).join("")}</div>`;
    if (other.length) body += `<h4>Other files</h4><div class="flist">${other.map(f => fileRow(f, esc(fileLabel(f)))).join("")}</div>`;
  }
  const isLoose = it.loose; // "Singles" lives in the artist folder: delete track by track
  d.innerHTML = `<div class="hero ${m.colors ? "tinted" : ""}" style="${heroStyle(m)}">
      ${m.art && it.kind !== "album" ? `<img class="backdrop" src="${art(m.art, 1280, 720)}" alt="">` : ""}
      ${it.kind === "album" && (m.thumb || n.cover) ? `<img class="backdrop blur" src="${m.thumb ? art(m.thumb, 400, 400) : `/api/cover?p=${encodeURIComponent(n.cover)}`}" alt="">` : ""}
      <form method="dialog"><button class="icon close" aria-label="Close">${icon("x")}</button></form>
      <div class="hero-in">${poster(it, it.kind === "album" ? 360 : 300, it.kind === "album" ? 360 : 450)}
        <div class="htext"><div class="kicker">${kicker.map(esc).join(" · ")}</div>
          <h2 class="htitle">${esc(it.title)}</h2>${it.kind === "album" ? `<div class="hartist">${esc(it.artist)}${it.year ? ` · ${esc(it.year)}` : ""}</div>` : ""}
          ${m.tagline ? `<div class="tagline">${esc(m.tagline)}</div>` : ""}
          <div class="bdgs">${badges(it.info, it.kind)}</div></div></div></div>
    <div class="dbody">${body}
      <div class="dacts">
        <span class="dsize">${plural(n.files, "file")} · <span class="mono">${bytes(n.size)}</span>${n.added ? ` · added ${ago(n.added)}` : ""}</span>
        ${isLoose ? "" : `<button class="pill danger" data-del="${esc(n.path)}" data-name="${esc(it.title)}" data-size="${n.size}" data-files="${n.files}">Delete</button>`}</div></div>`;
  if (!refresh || !d.open) { d.showModal(); d.scrollTop = 0; }
  history.replaceState(null, "", "#item=" + encodeURIComponent(n.path));
}
$("#detail").addEventListener("close", () => history.replaceState(null, "", "#library"));
$("#detail").addEventListener("click", e => {
  if (e.target === e.currentTarget) { e.currentTarget.close(); return; } // tap outside
  if (e.target.closest(".summary.clamp")) { e.target.closest(".summary").classList.toggle("open"); return; }
  const b = e.target.closest("[data-del]"); if (b) deleteFromLibrary(b);
});

for (const b of $$("#libSec button")) b.onclick = () => {
  libSec = b.dataset.v;
  $$("#libSec button").forEach(x => x.classList.toggle("on", x === b));
  renderLibrary();
};
for (const b of $$("#libView button")) {
  b.classList.toggle("on", b.dataset.v === libView);
  b.onclick = () => { libView = localStorage.libView = b.dataset.v; $$("#libView button").forEach(x => x.classList.toggle("on", x === b)); renderLibrary(); };
}
$("#libSort").onchange = renderLibrary;
$("#libSearch").oninput = renderLibrary;
document.addEventListener("keydown", e => { // "/" jumps to search
  if (e.key === "/" && !e.target.closest("input, textarea") && !$("dialog[open]")) {
    e.preventDefault();
    if (tab !== "library") $(".tabs [data-tab=library]").click();
    $("#libSearch").focus();
  }
});

// ---------- settings ----------
function openSettings() {
  if (!S) return;
  const s = S.settings;
  for (const o of $$("#settings .opts")) {
    const cur = s[o.dataset.key];
    for (const b of $$("button", o)) b.classList.toggle("on", Number(b.dataset.v) === cur);
  }
  $("#autoThrottle").checked = s.auto_throttle;
  $("#settings").showModal();
}
$("#openSettings").onclick = openSettings;
for (const o of $$("#settings .opts")) for (const b of $$("button", o)) b.onclick = async () => {
  $$("button", o).forEach(x => x.classList.toggle("on", x === b));
  await saveSetting({ [o.dataset.key]: Number(b.dataset.v) });
};
$("#autoThrottle").onchange = e => saveSetting({ auto_throttle: e.target.checked });
async function saveSetting(patch) {
  const r = await api("/api/settings", patch);
  if (r.error) toast(r.error, true); else { S.settings = r; renderStatus(); toast("Saved"); }
}

// ---------- start ----------
// links arrive as ?url= / ?text= from the bookmarklet, share sheet or Android share target
const qs = new URLSearchParams(location.search);
const shared = [qs.get("url"), qs.get("text"), qs.get("title")].filter(Boolean).join(" ");
if (shared) { history.replaceState(null, "", "/"); ta.value = shared; grow(); check(); }
if ("serviceWorker" in navigator && window.isSecureContext) navigator.serviceWorker.register("/sw.js");
connect();
if (location.hash === "#library" || wantItem) $(".tabs [data-tab=library]").click();
addEventListener("hashchange", () => {
  if (!location.hash.startsWith("#item=")) return;
  const p = decodeURIComponent(location.hash.slice(6));
  if (DETAIL?.node.path === p && $("#detail").open) return;
  wantItem = p;
  tab === "library" ? loadLibrary() : $(".tabs [data-tab=library]").click();
});
