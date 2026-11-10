"use strict";
// Media Core in a browser: the TV app's screens (docs/design-system.md), moved to a mouse and a keyboard.
// Home, Films, Shows, Search and title pages live here, the player in player.js, Manager in manager.js.
const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const icon = n => `<svg class="i" aria-hidden="true"><use href="#i-${n}"/></svg>`;
const img = (path, size) => path ? `/api/v1/img/${size}${path}` : "";

async function api(path, body, opts = {}) {
  try {
    const r = await fetch(path, body === undefined ? {} : { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body), ...opts });
    const j = await r.json().catch(() => ({ error: `HTTP ${r.status}` }));
    if (!r.ok && !j.error) j.error = `HTTP ${r.status}`;
    return j;
  } catch (e) {
    return { error: "Can't reach mint", offline: true };
  }
}
const store = {
  get(k) { try { return localStorage.getItem(k); } catch { return null; } },
  set(k, v) { try { localStorage.setItem(k, v); } catch {} },
};

let noteT;
function note(msg) {
  const n = $("#note");
  n.textContent = msg; n.classList.add("on");
  clearTimeout(noteT); noteT = setTimeout(() => n.classList.remove("on"), 2800);
}

// ---------- words ----------
const hm = ms => { const m = Math.floor(ms / 60000); return m >= 60 ? `${Math.floor(m / 60)}h ${m % 60}m` : `${m}m`; };
const clock = ms => {
  const s = Math.floor(Math.max(0, ms) / 1000), two = n => String(n).padStart(2, "0");
  return s >= 3600 ? `${Math.floor(s / 3600)}:${two(Math.floor(s / 60) % 60)}:${two(s % 60)}` : `${Math.floor(s / 60)}:${two(s % 60)}`;
};
function mins(ms) { const m = Math.round(ms / 60000); return m >= 60 ? `${Math.floor(m / 60)} h ${m % 60} min` : `${m} min`; }
const leftText = ms => ms < 60000 ? "less than a minute left" : mins(ms) + " left";
const durMs = it => it.watch?.dur > 0 ? it.watch.dur : (it.info?.dur || 0) * 1000;
const pos = it => it.watch?.pos || 0;
const progress = it => { const d = durMs(it); return pos(it) > 0 && d > 0 ? Math.min(1, pos(it) / d) : 0; };
const resumable = it => it && pos(it) > 0 && !it.watch?.watched && durMs(it) > pos(it);
const showTitle = it => it.show || displayTitle(it);
const displayTitle = it => (it.type !== "episode" && it.meta?.title) || it.title;
const displayYear = it => it.year > 0 ? it.year : it.meta?.year || 0;
const epLine = it => `S${it.season}, E${it.episode} · ${it.title}`;
const gb = b => b >= 1e9 ? (b / 1e9).toFixed(1) + " GB" : Math.round(b / 1e6) + " MB";

// ---------- pieces ----------
const tabsDef = [["#/search", "Search"], ["#/", "Home"], ["#/films", "Films"], ["#/shows", "Shows"]];
function tabs(current) {
  const busy = window.Manager?.busy?.();
  return `<nav class="tabs">${tabsDef.map(([h, l]) => `<a class="tab ${l === current ? "on" : ""}" href="${h}">${l}</a>`).join("")}
    <a class="tab ${current === "Manager" ? "on" : ""}" href="#/manager" id="mgrTab">${busy ? `<i class="dot"></i>` : ""}Manager</a></nav>`;
}
const track = f => `<div class="track"><i style="width:${(f * 100).toFixed(1)}%"></i></div>`;

function posterCard(it) {
  return `<a class="card poster p" href="#/title/${it.id}" title="${esc(displayTitle(it))}">${it.meta?.poster
    ? `<img src="${img(it.meta.poster, "w342")}" alt="${esc(displayTitle(it))}" loading="lazy" decoding="async">`
    : `<span class="none">${esc(displayTitle(it))}</span>`}</a>`;
}
function wideCard(it) {
  const pic = img(it.meta?.backdrop, "w500") || img(it.meta?.still, "w500");
  const left = pos(it) > 0 && durMs(it) > pos(it) ? leftText(durMs(it) - pos(it)) : "";
  return `<div class="w"><button class="card wide" data-open="${it.id}" data-resume="1">${pic ? `<img src="${pic}" alt="" loading="lazy" decoding="async">` : ""}${progress(it) ? track(progress(it)) : ""}</button>
    <div class="under meta"><span class="one">${esc(showTitle(it))}</span>${left ? `<span class="one mut" style="flex:none"> · ${esc(left)}</span>` : ""}</div></div>`;
}
function row(title, list, make) {
  if (!list?.length) return "";
  return `<section class="row"><h2 class="rowt heading">${esc(title)}</h2>
    <button class="shift l" tabindex="-1" aria-label="Back">${icon("ExpandMore")}</button>
    <div class="cards">${list.map(make).join("")}</div>
    <button class="shift r" tabindex="-1" aria-label="More">${icon("ExpandMore")}</button></section>`;
}
function wireRows(root) {
  for (const r of $$(".row", root)) {
    const c = $(".cards", r), l = $(".shift.l", r), rr = $(".shift.r", r);
    if (!l) continue;
    const up = () => {
      l.classList.toggle("can", c.scrollLeft > 2);
      rr.classList.toggle("can", c.scrollLeft + c.clientWidth < c.scrollWidth - 2);
    };
    c.addEventListener("scroll", up, { passive: true }); up();
    l.onclick = () => c.scrollBy({ left: -c.clientWidth * .8, behavior: "smooth" });
    rr.onclick = () => c.scrollBy({ left: c.clientWidth * .8, behavior: "smooth" });
  }
}
function titleMark(logo, name) {
  return `<div class="mark">${logo ? `<img src="${img(logo, "w500")}" alt="${esc(name)}">` : `<div class="display two" style="min-height:0">${esc(name)}</div>`}</div>`;
}
const MARKS = { atmos: [676, "atmos"], dv: [640, "dv"], dtsx: [269, "dtsx"], dtshdma: [321, "dtshdma"], truehd: [382, "truehd"], hdr10: [275, "hdr10"], hdr10p: [313, "hdr10p"] };
const logoMark = (k, h) => {
  const [w] = MARKS[k], px = 1.4 + h;
  return `<i class="logo" style="--m:url(/marks/logo_${k}.png);height:${px}rem;width:${(px * w / 96).toFixed(2)}rem"></i>`;
};
function formatMarks(i) {
  if (!i) return "";
  const out = [], hdr = i.hdr || "", a = i.audio || "";
  if (i.res === "4K") out.push(`<span class="chip">4K</span>`);
  if (hdr.startsWith("Dolby Vision")) out.push(logoMark("dv", 0));
  else if (hdr === "HDR10+") out.push(logoMark("hdr10p", .2));
  else if (hdr === "HDR10") out.push(logoMark("hdr10", .2));
  else if (hdr) out.push(`<span class="meta mut">${esc(hdr)}</span>`);
  if (a.includes("Atmos")) out.push(logoMark("atmos", 0));
  else if (a.startsWith("DTS:X")) out.push(logoMark("dtsx", .2));
  else if (a.startsWith("DTS-HD MA")) out.push(logoMark("dtshdma", .4));
  else if (a.startsWith("TrueHD")) out.push(logoMark("truehd", .4));
  return out.join("");
}
const page = () => $("#page");
function show(html) {
  const p = page();
  p.innerHTML = html;
  p.style.animation = "none"; void p.offsetWidth; p.style.animation = ""; // fades up again
  wireRows(p);
}
function loadingPage() {
  page().innerHTML = `<div class="centre ring-later"><div class="loading"></div></div>`;
}
function errorPage(retry) {
  show(`<div class="centre"><div class="title">Can't reach mint</div>
    <p class="mut" style="margin:1.2rem 0 2.4rem">At home: check that mint is on.<br>Away from home: open Tailscale and connect.</p>
    <button class="tool" id="retry" aria-label="Try again" title="Try again">${icon("Refresh")}</button></div>`);
  $("#retry").onclick = retry;
}

// ---------- Home ----------
let homeJson = "", homeT;
async function home(refresh) {
  if (!refresh && !homeJson) loadingPage();
  const h = await api("/api/v1/home");
  if (h.error) { if (!homeJson) errorPage(() => home()); return; }
  const text = JSON.stringify(h);
  if (refresh && text === homeJson) return;
  const here = route.key === "home";
  if (!here) return;
  homeJson = text;
  const keep = refresh ? $$(".cards", page()).map(c => c.scrollLeft) : null;
  const shows = h.shows || [], movies = h.movies || [];
  const f = [...(h.continue || []), ...(h.recent || []), ...shows, ...movies][0];
  const none = !movies.length && !shows.length ? `<p class="mut" style="padding:0 var(--side)">Nothing in the library yet. <a href="#/manager" style="color:var(--ink)">Add links</a>.</p>` : "";
  const html = tabs("Home") + (f ? stage(f, shows) : "") +
    row("Continue Watching", h.continue, wideCard) + row("Recently Added", h.recent, posterCard) +
    row("Shows", shows, posterCard) + row("Films", movies, posterCard) + none;
  if (refresh) { page().innerHTML = html; wireRows(page()); $$(".cards", page()).forEach((c, i) => c.scrollLeft = keep[i] || 0); }
  else show(html);
}
function stage(it, shows) {
  const show = it.type === "episode" ? shows.find(s => s.id === it.show_id) : null;
  const base = show || it;
  const pic = img(it.meta?.backdrop || base.meta?.backdrop, "w1280");
  const facts = [displayYear(base) || "", base.meta?.genres?.[0], base.meta?.cert || it.meta?.cert].filter(Boolean);
  const story = it.meta?.overview || base.meta?.overview;
  const ep = it.type === "episode";
  return `<section class="stage"><div class="words">
      ${titleMark(it.meta?.logo || show?.meta?.logo, showTitle(it))}
      ${facts.length ? `<div class="meta mut" style="margin-top:1.2rem">${facts.map(esc).join(" · ")}</div>` : ""}
      ${ep ? `<div class="label one" style="margin-top:1.2rem">${esc(epLine(it))}</div>` : ""}
      ${story ? `<p class="body mut two" style="margin-top:${ep ? ".4" : "1.2"}rem">${esc(story)}</p>` : ""}
      <div class="btns"><button class="play" data-open="${it.id}" data-resume="1">${icon("Play")}<span class="fig">${resumable(it) ? hm(durMs(it) - pos(it)) : "Play"}</span></button>
        <a class="tool" href="#/title/${it.show_id || it.id}" aria-label="More about this" title="More about this">${icon("Info")}</a></div>
    </div><div class="frame">${pic ? `<img src="${pic}" alt="">` : ""}</div></section>`;
}

// Continue Watching and the featured title play straight away, when this browser has something to play
document.addEventListener("click", async e => {
  const b = e.target.closest("[data-open]");
  if (!b) return;
  const id = b.dataset.open;
  const it = (await api(`/api/v1/item/${id}`)).item;
  if (!it) return;
  const st = await webAPI(`/api/v1/web/${id}`);
  if (st.state === "ready" || st.state === "wait") { location.hash = `#/play/${id}`; return; }
  location.hash = `#/title/${it.show_id || id}`;
});

// ---------- Films, Shows ----------
async function library(shows) {
  const key = route.key;
  loadingPage();
  const r = await api(shows ? "/api/v1/shows" : "/api/v1/movies");
  if (route.key !== key) return;
  if (r.error) { errorPage(() => library(shows)); return; }
  const items = r.items || [];
  show(tabs(shows ? "Shows" : "Films") + (items.length
    ? `<div class="grid">${items.map(posterCard).join("")}</div>` : `<p class="mut" style="padding:0 var(--side)">Nothing here yet.</p>`));
}

// ---------- Search ----------
let lastQuery = "";
async function search() {
  show(tabs("Search") + `<label class="query">${icon("Search")}<input id="q" type="search" placeholder="Search" aria-label="Search titles and cast" autocomplete="off" spellcheck="false" value="${esc(lastQuery)}"></label><div id="found" class="grid"></div>`);
  const q = $("#q"); q.focus();
  const [mv, sh] = await Promise.all([api("/api/v1/movies"), api("/api/v1/shows")]);
  const all = [...(sh.items || []), ...(mv.items || [])];
  let t;
  const run = async () => {
    lastQuery = q.value;
    const text = q.value.trim(), box = $("#found");
    if (!box) return;
    if (!text) { box.innerHTML = all.map(posterCard).join(""); return; }
    const r = await api(`/api/v1/search?q=${encodeURIComponent(text)}`);
    if (q.value.trim() !== text || !$("#found")) return;
    box.innerHTML = r.items?.length ? r.items.map(posterCard).join("") : `<p class="mut" style="grid-column:1/-1">No matches for “${esc(text)}”.</p>`;
  };
  q.oninput = () => { clearTimeout(t); t = setTimeout(run, 250); };
  run();
}

// ---------- title pages ----------
const QUALITY = { "4K": "4K Ultra HD", "1080p": "Full HD", "720p": "HD" };
const codecName = v => v.includes("Main 10") ? v.split(" Main")[0] + ", 10-bit" : (v.includes(" Main") || v.includes(" High")) ? v.split(" ")[0] : v;
const lengthName = sec => { const s = Math.floor(sec); return [s >= 3600 ? Math.floor(s / 3600) + "h" : "", Math.floor(s / 60) % 60 + "m", s % 60 + "s"].filter(Boolean).join(" "); };
function audioLine(t) {
  const [fmt, lang] = t.split(" · ");
  const f = fmt.replace("DD+", "Dolby Digital+").replace("DD ", "Dolby Digital ").replace("stereo", "Stereo").replace("mono", "Mono");
  return lang ? [lang, f] : ["Audio", f];
}

async function title(id) {
  const key = route.key;
  loadingPage();
  const [d, web] = await Promise.all([api(`/api/v1/item/${id}`), webAPI("/api/v1/web")]);
  if (route.key !== key) return;
  if (d.error) { d.offline ? errorPage(() => title(id)) : show(tabs("") + `<div class="centre"><div class="title">Not in the library</div></div>`); return; }
  const main = d.item, up = d.up_next, isShow = main.type === "show";
  const ready = web.ready || {};
  const target = isShow || main.type === "season" ? up : main;
  const seasons = main.seasons || [];
  let season = Math.max(0, seasons.findIndex(s => up && (s.episodes || []).some(e => e.id === up.id)));
  let described = target;

  const words = t => {
    const ep = isShow ? t : null;
    const story = (ep && ep.meta?.overview) || main.meta?.overview || "";
    const len = (t && durMs(t)) || ((t || main).meta?.runtime || 0) * 60000;
    const facts = [displayYear(main) || "", len ? hm(len) : ""].filter(Boolean).join(" · ");
    return `${ep ? `<div class="label one" style="margin-top:1.2rem">${esc(epLine(ep))}</div>` : ""}
      <p class="body mut two" style="margin-top:${ep ? ".4" : "1.2"}rem">${esc(story)}</p>
      <div class="facts" style="margin-top:.8rem">${facts ? `<span class="meta mut fig">${esc(facts)}</span>` : ""}${formatMarks((t || main).info)}</div>`;
  };
  const allWatched = () => isShow ? !main.unseen : main.watch?.watched;
  const buttons = () => {
    const can = target && ready[target.id], res = resumable(target);
    return `<button class="play" id="pPlay" ${can ? "" : "disabled"}>${icon("Play")}<span class="fig">${!can ? "Not ready for browsers" : res ? hm(durMs(target) - pos(target)) : "Play"}</span></button>
      ${res && can ? `<button class="tool" id="pOver" title="Start over" aria-label="Start over">${icon("Replay")}</button>` : ""}
      <button class="tool ${allWatched() ? "" : "dim"}" id="pSeen" title="${allWatched() ? "Mark unwatched" : "Mark watched"}" aria-label="${allWatched() ? "Mark unwatched" : "Mark watched"}">${icon("Check")}</button>
      <button class="tool" id="pInfo" title="Details" aria-label="Details">${icon("Info")}</button>`;
  };
  const episodes = () => {
    const s = seasons[season]; if (!s) return "";
    return `<section class="row sect"><button class="season heading ${seasons.length > 1 ? "many" : ""}" id="pSeason" ${seasons.length > 1 ? "" : "disabled"}>${esc(s.title)}${seasons.length > 1 ? icon("ExpandMore") : ""}</button>
      <button class="shift l" tabindex="-1" aria-label="Back">${icon("ExpandMore")}</button>
      <div class="cards">${(s.episodes || []).map(e => `<div class="w"><button class="card wide" data-ep="${e.id}" aria-label="${esc(e.title)}">
          ${e.meta?.still || e.meta?.backdrop ? `<img src="${img(e.meta.still || e.meta.backdrop, "w500")}" alt="" loading="lazy" decoding="async">` : ""}
          ${!e.watch?.watched && resumable(e) ? track(progress(e)) : ""}</button>
        <div class="under meta"><span class="one">${e.episode} · ${esc(e.title)}</span>${e.watch?.watched ? `<span class="check" title="Watched">${icon("Check")}</span>` : ""}</div></div>`).join("")}</div>
      <button class="shift r" tabindex="-1" aria-label="More">${icon("ExpandMore")}</button></section>`;
  };
  const cast = () => {
    const c = main.meta?.cast || [];
    if (!c.length) return "";
    return `<section class="sect"><h2 class="heading" style="padding:0 var(--side) 1.6rem">Cast</h2><div class="cast">${c.slice(0, 8).map(p => `<div>
      <div class="face">${p.photo ? `<img src="${img(p.photo, "w185")}" alt="" loading="lazy">` : `<span class="label mut">${esc(p.name.split(" ").map(x => x[0]).slice(0, 2).join(""))}</span>`}</div>
      <div class="meta one">${esc(p.name)}</div>${p.role ? `<div class="caption mut one">${esc(p.role)}</div>` : ""}</div>`).join("")}</div></section>`;
  };
  const kind = isShow ? "Show" : "Film";
  const head = [kind, ...(main.meta?.genres || []).slice(0, 2), main.meta?.cert].filter(Boolean).map(esc).join(" · ");
  const draw = () => {
    show(`<div class="tpage">${main.meta?.backdrop ? `<div class="banner"><img src="${img(main.meta.backdrop, "original")}" alt=""></div>` : ""}
      <button class="ctl pback" id="pBack" aria-label="Back" title="Back (Esc)">${icon("Back")}</button>
      <div class="top">${titleMark(main.meta?.logo, displayTitle(main))}
        <div class="meta mut" style="margin-top:1.2rem">${head}</div>
        <div id="words">${words(described)}</div><div class="btns" id="btns">${buttons()}</div></div>
      <div id="below">${isShow ? episodes() : cast()}</div></div>`);
    wire();
  };
  const wire = () => {
    const at = (s, fn) => { const el = $(s); if (el) el.onclick = fn; };
    wireButtons();
    at("#pBack", goBack);
    at("#pSeason", () => seasonMenu());
    for (const c of $$("[data-ep]")) {
      const ep = seasons[season].episodes.find(e => e.id === c.dataset.ep);
      const hover = () => { described = ep; $("#words").innerHTML = words(ep); };
      c.addEventListener("mouseenter", hover); c.addEventListener("focus", hover);
      c.onclick = () => ready[ep.id] ? location.hash = `#/play/${ep.id}` : note("This episode isn't ready for browsers yet.");
    }
    const bl = $("#below .row");
    if (bl) bl.addEventListener("mouseleave", () => { described = target; $("#words").innerHTML = words(target); });
  };
  const wireButtons = () => {
    const at = (s, fn) => { const el = $(s); if (el) el.onclick = fn; };
    at("#pPlay", () => { location.hash = `#/play/${target.id}`; });
    at("#pOver", () => { location.hash = `#/play/${target.id}/start`; });
    at("#pSeen", async () => {
      await api("/api/v1/watched", { id: main.id, watched: !allWatched() });
      const r = await api(`/api/v1/item/${id}`);
      if (r.item) { Object.assign(main, r.item); $("#btns").innerHTML = buttons(); wireButtons(); }
    });
    at("#pInfo", () => details(described || main, main));
  };
  const seasonMenu = () => {
    const scrim = document.createElement("div"); scrim.className = "scrim"; scrim.style.background = "transparent";
    const m = document.createElement("div"); m.className = "menu seasons";
    m.innerHTML = seasons.map((s, i) => `<button class="opt" data-i="${i}"><span class="tick">${i === season ? icon("Check") : ""}</span><span class="name one">${esc(s.title)}</span></button>`).join("");
    document.body.append(scrim, m);
    const close = () => { scrim.remove(); m.remove(); removeEventListener("keydown", onKey, true); $("#pSeason")?.focus(); };
    const onKey = e => { if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); close(); } };
    addEventListener("keydown", onKey, true);
    scrim.onclick = close;
    m.onclick = e => {
      const b = e.target.closest("[data-i]"); if (!b) return;
      season = +b.dataset.i; close();
      $("#below").innerHTML = episodes(); wireRows($("#below")); wire();
    };
    ($(`[data-i="${season}"]`, m) || m.firstChild).focus();
  };
  draw();
}

// The numbers, one press away: a sheet over the page's blurred picture.
function details(it, main) {
  const i = it.info || main.info || {};
  const pic = img(main.meta?.backdrop, "w780");
  const fig = (v, n) => `<div><div class="title fig one">${esc(v)}</div><div class="caption one">${esc(n)}</div></div>`;
  const group = (h, rows) => rows.length ? `<div class="group"><div class="caption">${h}</div>${rows.map(([a, b]) => `<div class="spec"><span class="body">${esc(a)}</span><span class="label fig one" title="${esc(b)}">${esc(b)}</span></div>`).join("")}</div>` : "";
  const tracks = (i.tracks?.length ? i.tracks : i.audio ? [i.audio] : []).map(audioLine).sort((a, b) => (b[0] === "English") - (a[0] === "English"));
  const subs = [...(i.subs || []), ...(it.subs || []).map(s => s.label)].filter(Boolean);
  const langs = subs.length ? [...tracks, ["Subtitles", subs.some(s => /english/i.test(s)) ? "English" : "No English"]] : tracks;
  const picture = [i.w > 0 && i.h > 0 ? ["Resolution", `${i.w} × ${i.h}`] : null, i.vcodec ? ["Video", codecName(i.vcodec)] : null, i.hdr ? ["HDR", i.hdr] : null].filter(Boolean);
  const file = [it.size > 0 ? ["Size", gb(it.size)] : null, i.dur > 0 ? ["Length", lengthName(i.dur)] : null].filter(Boolean);
  const scrim = document.createElement("div"); scrim.className = "scrim";
  const sheetBg = document.createElement("div"); sheetBg.className = "scrim";
  sheetBg.style.background = "var(--base)"; sheetBg.style.overflow = "hidden";
  sheetBg.innerHTML = pic ? `<div class="sheetbg" style="background-image:url('${pic}')"></div>` : "";
  scrim.style.zIndex = 30;
  const s = document.createElement("aside"); s.className = "sheet"; s.tabIndex = -1;
  s.innerHTML = `<div class="head"><div style="min-width:0"><h2 class="title">Details</h2>${it.type === "episode" ? `<div class="meta mut one" style="margin-top:.4rem">${esc(epLine(it))}</div>` : ""}</div>
      <button class="tool" aria-label="Close" title="Close">${icon("Close")}</button></div>
    <div class="figures">${i.res ? fig(i.res, QUALITY[i.res] || i.res) : ""}${i.mbps > 0 ? fig(i.mbps.toFixed(1), "Mb/s") : ""}${i.fps > 0 ? fig(i.fps.toFixed(3).replace(".000", ""), "fps") : ""}</div>
    ${group("Picture", picture)}${group("Languages", langs)}${group("File", file)}`;
  document.body.append(sheetBg, scrim, s);
  s.focus();
  const close = () => { sheetBg.remove(); scrim.remove(); s.remove(); removeEventListener("keydown", onKey, true); $("#pInfo")?.focus(); };
  const onKey = e => {
    if (["Escape", "ArrowLeft", "ArrowRight", "Enter"].includes(e.key)) { e.preventDefault(); e.stopPropagation(); close(); }
  };
  addEventListener("keydown", onKey, true);
  scrim.onclick = close; $(".tool", s).onclick = close;
}

// Back to where you came from; Esc does the same on a title page, as Back does on the remote.
function goBack() { if (history.length > 1) history.back(); else location.hash = "#/"; }
document.addEventListener("keydown", e => {
  if (e.key === "Escape" && route.key.startsWith("title") && !$("#player") && !$(".scrim")) { e.preventDefault(); goBack(); }
});

// ---------- the keyboard: arrows move between things, as the remote does ----------
function focusables() {
  return $$("a[href], button:not([disabled]):not([tabindex='-1']), input, textarea, select", page())
    .filter(el => { const r = el.getBoundingClientRect(); return r.width > 0 && r.height > 0; });
}
document.addEventListener("keydown", e => {
  if (!["ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight"].includes(e.key) || e.ctrlKey || e.metaKey || e.altKey) return;
  if ($("#player") || $(".scrim") || route.key === "manager") return;
  const a = document.activeElement;
  if (a && a.matches("input, textarea, select") && (e.key === "ArrowLeft" || e.key === "ArrowRight")) return;
  const all = focusables();
  if (!all.length) return;
  e.preventDefault();
  if (!all.includes(a)) { (all.find(x => x.closest(".btns, .cards, .grid")) || all[0]).focus(); return; }
  const from = a.getBoundingClientRect(), fx = from.left + from.width / 2, fy = from.top + from.height / 2;
  const dir = e.key;
  let best = null, bestD = Infinity;
  for (const el of all) {
    if (el === a) continue;
    const r = el.getBoundingClientRect(), x = r.left + r.width / 2, y = r.top + r.height / 2;
    const dx = x - fx, dy = y - fy;
    const on = dir === "ArrowLeft" ? dx < -1 && r.right <= from.left + 2 : dir === "ArrowRight" ? dx > 1 && r.left >= from.right - 2
      : dir === "ArrowUp" ? dy < -1 && r.bottom <= from.top + 2 : dy > 1 && r.top >= from.bottom - 2;
    if (!on) continue;
    const d = dir === "ArrowLeft" || dir === "ArrowRight" ? Math.abs(dx) + Math.abs(dy) * 3 : Math.abs(dy) + Math.abs(dx) * .5;
    if (d < bestD) { bestD = d; best = el; }
  }
  if (best) { best.focus({ preventScroll: true }); best.scrollIntoView({ block: "center", inline: "nearest", behavior: "smooth" }); }
});

// ---------- where we are ----------
const scrolls = {};
let current = "";
function route() {
  const h = location.hash || "#/";
  const parts = h.slice(2).split("/");
  scrolls[current] = scrollY;
  const leavingPlayer = route.key === "play" && parts[0] !== "play";
  if (route.key === "play" && parts[0] !== "play") Player.close();
  clearInterval(homeT);
  if (parts[0] === "play") {
    route.key = "play";
    Player.open(parts[1], { fromStart: parts[2] === "start" });
    return;
  }
  current = h;
  if (leavingPlayer && page().innerHTML) homeJson = "";
  switch (parts[0]) {
    case "": route.key = "home"; homeJson = ""; home(); homeT = setInterval(() => document.visibilityState === "visible" && home(true), 60000); break;
    case "films": route.key = "films"; library(false); break;
    case "shows": route.key = "shows"; library(true); break;
    case "search": route.key = "search"; search(); break;
    case "title": route.key = "title" + parts[1]; title(parts[1]); break;
    case "manager": route.key = "manager"; Manager.page(); break;
    case "add": location.replace("#/manager"); break;
    default: location.hash = "#/";
  }
  requestAnimationFrame(() => scrollTo(0, scrolls[h] || 0));
}
addEventListener("hashchange", route);
// unlock the player's video on any click or Enter, so Safari lets it start once it has loaded (player.js, prime)
addEventListener("click", Player.prime, true);
addEventListener("keydown", e => { if (e.key === "Enter" || e.key === " ") Player.prime(); }, true);

// links arrive as ?url= / ?text= from the bookmarklet, share sheet or Android share target
const qs = new URLSearchParams(location.search);
const shared = [qs.get("url"), qs.get("text"), qs.get("title")].filter(Boolean).join(" ");
if (shared) { window.sharedLinks = shared; history.replaceState(null, "", "/#/manager"); }
if ("serviceWorker" in navigator && window.isSecureContext) navigator.serviceWorker.register("/sw.js");
Manager.connect();
route();
