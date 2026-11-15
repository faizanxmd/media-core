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

// The accent, chosen in Settings, as on the TV: Play, progress and Settings' headings. Kept in this browser.
// main, under the pointer, progress, as words, words on it. The player keeps its own look.
const ACCENTS = { Blue: ["#1B34D6", "#2643EE", "#3355FF", "#7C95FF", "#FFFFFF"], White: ["#F2F3F7", "#FFFFFF", "#F2F3F7", "#F2F3F7", "#090B0E"],
  Ember: ["#C2521D", "#D6612A", "#E8692F", "#F2945F", "#FFFFFF"], Red: ["#D20812", "#E81620", "#E50914", "#FF6B70", "#FFFFFF"] };
function applyAccent(name) {
  const a = ACCENTS[name] || ACCENTS.Blue, r = document.documentElement.style;
  ["--accent", "--accent-up", "--bar", "--word", "--on-accent"].forEach((k, i) => r.setProperty(k, a[i]));
}
applyAccent(store.get("accent"));

let noteT;
// note: a line at the foot of the page. bad: something went wrong, so it stays until it's read (a click) or 12 s pass.
function note(msg, bad) {
  const n = $("#note");
  n.textContent = msg; n.classList.add("on"); n.classList.toggle("bad", !!bad);
  n.onclick = () => n.classList.toggle("on", false);
  clearTimeout(noteT); noteT = setTimeout(() => n.classList.toggle("on", false), bad ? 12000 : 2800);
}

// ---------- words ----------
const hm = ms => { const m = Math.floor(ms / 60000); return m >= 60 ? `${Math.floor(m / 60)}h ${m % 60}m` : `${m}m`; };
const clock = ms => {
  const s = Math.floor(Math.max(0, ms) / 1000), two = n => String(n).padStart(2, "0");
  return s >= 3600 ? `${Math.floor(s / 3600)}:${two(Math.floor(s / 60) % 60)}:${two(s % 60)}` : `${Math.floor(s / 60)}:${two(s % 60)}`;
};
function mins(ms) { const m = Math.round(ms / 60000); return m >= 60 ? `${Math.floor(m / 60)} h${m % 60 ? ` ${m % 60} min` : ""}` : `${m} min`; }
const leftText = ms => ms < 60000 ? "less than a minute left" : mins(ms) + " left";
const durMs = it => it.watch?.dur > 0 ? it.watch.dur : (it.info?.dur || 0) * 1000;
const pos = it => it.watch?.pos || 0;
const progress = it => { const d = durMs(it); return pos(it) > 0 && d > 0 ? Math.min(1, pos(it) / d) : 0; };
const resumable = it => it && pos(it) > 0 && !it.watch?.watched && durMs(it) > pos(it);
// worth saying "Resume" for: a few minutes in is still just "Play" (it still picks up where it stopped)
const begun = it => resumable(it) && progress(it) >= .05;
const leftNote = it => begun(it) ? `<span class="left fig">${esc(leftText(durMs(it) - pos(it)))}</span>` : "";
const showTitle = it => it.show || displayTitle(it);
const displayTitle = it => (it.type !== "episode" && it.meta?.title) || it.title;
const displayYear = it => it.year > 0 ? it.year : it.meta?.year || 0;
const epLine = it => `S${it.season}, E${it.episode} · ${it.title}`;
// over a title's story: a real name with its place, a nameless one as just its place ("Season 1, Episode 2", never "E2 · Episode 2")
const epWords = it => it.title && it.title.toLowerCase() !== `episode ${it.episode}` ? epLine(it) : `Season ${it.season}, Episode ${it.episode}`;
const gb = b => b >= 1e9 ? (b / 1e9).toFixed(1) + " GB" : Math.round(b / 1e6) + " MB";

// ---------- what plays on this device ----------
// The engine decides (play.go). Nearly everything plays, so only the exceptions are marked:
// a poster that can't play here is dimmed and says so, and its page says why.
let playsAt = 0, playsMap = { ready: {}, blocked: {} };
async function plays() {
  if (Date.now() - playsAt < 30000) return playsMap;
  const r = await webAPI("/api/v1/web");
  if (!r.error) { playsMap = { ready: r.ready || {}, blocked: r.blocked || {} }; playsAt = Date.now(); }
  return playsMap;
}
const cantPlay = it => it && playsMap.blocked[it.id];
const cantChip = it => cantPlay(it) ? `<span class="cant" title="${esc("Doesn't play on this device: it can't show " + cantPlay(it))}">${icon("No")}</span>` : "";

// ---------- pieces ----------
// The bar, as on the TV: the wordmark on the left (it goes Home), Settings on the right. Nothing else: everything is on Home.
function tabs(current) {
  $("#topbar").innerHTML = `<a class="brand" href="#/" aria-label="Media Core, Home"><img src="/wordmark.svg" alt="" width="89" height="34"></a>
    <span class="sp"></span><a class="tab ${current === "Settings" || current === "Manager" ? "on" : ""}" href="#/settings">${icon("Settings")}Settings</a>`;
  return "";
}
const barState = () => $("#topbar").classList.toggle("solid", scrollY > 24);
addEventListener("scroll", barState, { passive: true });
const track = f => `<div class="track"><b><i style="width:${(f * 100).toFixed(1)}%"></i></b></div>`;
// the picture across the top of a page: the full-size one on a sharp or wide screen
const bigPic = () => innerWidth * (devicePixelRatio || 1) > 1400 ? "original" : "w1280";
const art = path => `<div class="art">${path ? `<img src="${img(path, bigPic())}" alt="" onload="this.classList.add('in')">` : ""}</div>`;

// a poster; named: with its name and year under it, where titles are looked up rather than glanced at
function posterCard(it, named) {
  const name = esc(displayTitle(it));
  const pic = it.meta?.poster ? `<img src="${img(it.meta.poster, "w342")}" alt="${name}" loading="lazy" decoding="async">` : `<span class="none">${name}</span>`;
  if (!named) return `<a class="card poster p ${cantPlay(it) ? "dimmed" : ""}" href="#/title/${it.id}" title="${name}" data-hid="${it.id}">${cantChip(it)}${pic}</a>`;
  return `<a class="pc" href="#/title/${it.id}"><span class="card poster ${cantPlay(it) ? "dimmed" : ""}">${cantChip(it)}${pic}</span>
    <span class="label one">${name}</span><span class="meta mut fig">${displayYear(it) || ""}</span></a>`;
}
function wideCard(it) {
  const pic = img(it.meta?.backdrop, "w780") || img(it.meta?.still, "w780");
  const left = pos(it) > 0 && durMs(it) > pos(it) ? leftText(durMs(it) - pos(it)) : "";
  const sub = [it.type === "episode" ? `S${it.season}, E${it.episode}` : "", left].filter(Boolean).join(" · ");
  return `<div class="w" data-hid="${it.id}"><button class="card wide ${cantPlay(it) ? "dimmed" : ""}" data-open="${it.id}" data-resume="1" aria-label="${esc(showTitle(it))}">${cantChip(it)}${pic ? `<img src="${pic}" alt="" loading="lazy" decoding="async">` : ""}${progress(it) ? track(progress(it)) : ""}</button>
    <div class="label one">${esc(showTitle(it))}</div>${sub ? `<div class="meta mut one fig">${esc(sub)}</div>` : ""}</div>`;
}
function row(title, list, make) {
  if (!list?.length) return "";
  return `<section class="row"><h2 class="rowt">${esc(title)}</h2>
    <button class="shift l" tabindex="-1" aria-label="Back">${icon("ExpandMore")}</button>
    <div class="cards">${list.map(x => make(x)).join("")}</div>
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
// the format marks after the facts, set off by the same middle dot
const marksAfter = i => { const m = formatMarks(i); return m ? `<span class="marks">${m}</span>` : ""; };
const page = () => $("#page");
// a panel over the page closes when the page under it changes (the browser's Back, a link). Returns how to stop listening.
function onLeave(fn) { addEventListener("hashchange", fn, { once: true }); return () => removeEventListener("hashchange", fn); }
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
// As on the TV: the picture and words at the top belong to the card you're on (under the pointer, or the keyboard's),
// then Continue watching and Recently added. A title still downloading leads Recently added.
let homeJson = "", homeT, homeData = null, heroId = "", heroT;
async function home(refresh) {
  if (!refresh && !homeJson) loadingPage();
  const [h] = await Promise.all([api("/api/v1/home"), plays()]);
  if (h.error) { if (!homeJson) errorPage(() => home()); return; }
  const text = JSON.stringify(h);
  if (refresh && text === homeJson) return;
  if (route.key !== "home") return;
  homeJson = text; homeData = h;
  const keep = refresh ? $$(".cards", page()).map(c => c.scrollLeft) : null;
  const shows = h.shows || [], movies = h.movies || [], cont = h.continue || [], recent = h.recent || [];
  const dls = (h.downloading || []).map(d => ({ dl: d }));
  const kept = refresh ? [...cont, ...recent].find(x => x.id === heroId) : null;
  const f = kept || cont[0] || recent[0];
  heroId = f?.id || "";
  const none = !movies.length && !shows.length && !dls.length ? `<p class="mut plain" style="padding-left:var(--side)">Nothing in the library yet. <a href="#/manager" style="color:var(--ink)">Add links in Manager</a>.</p>` : "";
  const html = tabs("Home") + `<div class="home">${f ? stage(f, shows) : ""}${row("Continue watching", cont, wideCard)}${row("Recently added", [...dls, ...recent], x => x.dl ? dlCard(x.dl) : posterCard(x))}${none}</div>`;
  if (refresh) { page().innerHTML = html; wireRows(page()); $$(".cards", page()).forEach((c, i) => c.scrollLeft = keep[i] || 0); }
  else show(html);
}
// a title on its way: its name on a plain card, how far along inside its foot; it opens Manager
const dlCard = d => `<a class="card poster p dl" href="#/manager" title="${esc(d.title)}"><span class="dlw"><span class="caps">${d.state === "downloading" ? "Downloading" : "Getting ready"}</span><span class="name">${esc(d.title)}</span>${d.eta ? `<span class="meta mut fig">${esc(d.eta)} left</span>` : ""}</span>${track((d.progress || 0) / 100)}</a>`;
function stage(it, shows) {
  const show = it.type === "episode" ? shows.find(s => s.id === it.show_id) : null;
  const base = show || it;
  const pic = it.meta?.backdrop || base.meta?.backdrop;
  const facts = [displayYear(base) || "", ...(base.meta?.genres || []).slice(0, 2), base.meta?.cert || it.meta?.cert].filter(Boolean);
  const story = it.meta?.overview || base.meta?.overview;
  const ep = it.type === "episode";
  return `<div class="stage">${art(pic)}<section class="hero ${pic ? "" : "bare"}"><div class="words">
      ${titleMark(it.meta?.logo || show?.meta?.logo, showTitle(it))}
      <div class="facts">${facts.length ? `<span class="dots fig">${facts.map(x => `<span>${esc(x)}</span>`).join("")}</span>` : ""}${marksAfter(it.info)}</div>
      ${ep ? `<div class="label one epl">${esc(epWords(it))}</div>` : ""}
      ${story ? `<p class="story">${esc(story)}</p>` : ""}
      <div class="btns"><button class="play" data-open="${it.id}" data-resume="1">${icon("Play")}<span>${begun(it) ? "Resume" : "Play"}</span></button>
        <a class="word ic" href="#/title/${it.show_id || it.id}">${icon("Info")}More info</a>${leftNote(it)}</div>
    </div></section></div>`;
}
// The top follows the card you're on once you rest on it (300 ms, as the TV waits for the remote to settle):
// the new picture fades in over the old one, the words fade through.
function heroTo(id) {
  if (!homeData || id === heroId) return;
  const it = [...(homeData.continue || []), ...(homeData.recent || [])].find(x => x.id === id);
  const st = $(".home .stage");
  if (!it || !st) return;
  heroId = id;
  const next = document.createElement("div"); next.innerHTML = stage(it, homeData.shows || []);
  const box = $(".art", st), img = $(".art img", next), olds = $$("img", box);
  if (img) {
    img.removeAttribute("onload");
    const done = () => { img.classList.add("in"); setTimeout(() => olds.forEach(o => o.remove()), 520); };
    box.append(img);
    img.complete && img.naturalWidth ? requestAnimationFrame(done) : img.addEventListener("load", done, { once: true });
  } else olds.forEach(o => o.classList.remove("in"));
  const hero = $(".hero", st), nh = $(".hero", next);
  hero.classList.add("out");
  setTimeout(() => { hero.innerHTML = nh.innerHTML; hero.className = nh.className + " out"; requestAnimationFrame(() => requestAnimationFrame(() => hero.classList.remove("out"))); }, 150);
}
const heroFrom = e => {
  const c = e.target.closest?.(".home .cards [data-hid]");
  if (!c) return;
  clearTimeout(heroT); heroT = setTimeout(() => heroTo(c.dataset.hid), 300);
};
document.addEventListener("pointerover", heroFrom);
document.addEventListener("focusin", heroFrom);
document.addEventListener("pointerout", e => { if (e.target.closest?.(".home .cards [data-hid]") && !e.relatedTarget?.closest?.(".home .cards [data-hid]")) clearTimeout(heroT); });

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

// ---------- Settings ----------
// As on the TV: one list of what can be set, under headings in the accent. Only what works here is listed.
function settings() {
  const cur = ACCENTS[store.get("accent")] ? store.get("accent") : "Blue";
  show(tabs("Settings") + `<div class="plain setpage"><h1 class="ptitle">Settings</h1>
    <section><h2 class="sech">Look</h2>
      <div class="setline"><div><div class="label">Accent colour</div><small>Play, progress bars and these headings</small></div>
        <div class="seg swatches" id="accent">${Object.keys(ACCENTS).map(n => `<button class="${n === cur ? "on" : ""}" data-a="${n}"><i style="background:${ACCENTS[n][2]}"></i>${n}</button>`).join("")}</div></div></section>
    <section><h2 class="sech">Library</h2>
      <a class="setline go" href="#/manager"><div><div class="label">Manager</div><small>Add links, follow downloads, look after the files and mint</small></div>${icon("ExpandMore")}</a></section>
    <p class="tmdb">Film and show information from TMDB. This app is not endorsed or certified by TMDB.</p></div>`);
  for (const b of $$("#accent button")) b.onclick = () => {
    store.set("accent", b.dataset.a); applyAccent(b.dataset.a);
    $$("#accent button").forEach(x => x.classList.toggle("on", x === b));
  };
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
  const blocked = web.blocked || {};
  if (d.error) { d.offline ? errorPage(() => title(id)) : show(tabs("") + `<div class="centre"><div class="title">Not in the library</div></div>`); return; }
  const main = d.item, up = d.up_next, isShow = main.type === "show";
  const ready = web.ready || {};
  const target = isShow || main.type === "season" ? up : main;
  const seasons = main.seasons || [];
  let season = Math.max(0, seasons.findIndex(s => up && (s.episodes || []).some(e => e.id === up.id)));

  // what the words under the logo say: which episode Play starts, and the title's own story (each episode's is in the list)
  const words = () => {
    const ep = isShow ? target : null;
    const story = main.meta?.overview || "";
    return `${ep ? `<div class="label one epl">${esc(epWords(ep))}</div>` : ""}${story ? `<p class="story fold" id="pStory" title="Show it all">${esc(story)}</p>` : ""}`;
  };
  const allWatched = () => isShow ? !main.unseen : main.watch?.watched;
  const buttons = () => {
    const can = target && ready[target.id], res = begun(target);
    // Play in one word; how long is left follows the buttons as plain words. The rest are words, not riddles.
    const seen = allWatched();
    return `<button class="play" id="pPlay" ${can ? "" : "disabled"}>${icon("Play")}<span>${!can ? "Can't play here" : res ? "Resume" : "Play"}</span></button>
      ${res && can ? `<button class="word ic" id="pOver">${icon("Replay")}Start over</button>` : ""}
      <button class="word bare ic" id="pSeen" title="${seen ? "Mark unwatched" : "Mark watched"}">${icon("Check")}${seen ? "Watched" : "Mark watched"}</button>
      <button class="word bare ic" id="pInfo">${icon("Info")}Details</button>${can ? leftNote(target) : ""}`;
  };
  // episodes: a list down the page. Each: its still, "EPISODE 2" over a real name (a nameless one is just "Episode 2"),
  // two lines of story, and beside them one line: its length, what's left of it, or Watched.
  const episodes = () => {
    const s = seasons[season]; if (!s) return "";
    const list = seasons.map((x, i) => `<button class="sea ${i === season ? "on" : ""}" data-season="${i}"><span class="label">${esc(x.title)}</span><span class="meta fig">${(x.episodes || []).length} ${(x.episodes || []).length === 1 ? "episode" : "episodes"}</span></button>`).join("");
    return `<section class="sect"><h2 class="rowt">Episodes</h2><div class="split"><nav class="seasons">${list}</nav><div class="elist">${(s.episodes || []).map(e => {
        const len = durMs(e) || (e.meta?.runtime || 0) * 60000, part = !e.watch?.watched && resumable(e), next = up && e.id === up.id;
        const named = e.episode > 0 && e.title && e.title.toLowerCase() !== `episode ${e.episode}`;

        return `<button class="erow ${next ? "next" : ""}" data-ep="${e.id}">
          <span class="card wide">${e.meta?.still || e.meta?.backdrop ? `<img src="${img(e.meta.still || e.meta.backdrop, "w500")}" alt="" loading="lazy" decoding="async">` : ""}${part ? track(progress(e)) : ""}</span>
          <span class="txt">${named ? `<span class="caps fig">Episode ${e.episode}</span>` : ""}<span class="label one">${esc(e.title || `Episode ${e.episode}`)}</span>${e.meta?.overview ? `<p>${esc(e.meta.overview)}</p>` : ""}</span>
          <span class="end fig">${e.watch?.watched ? `<span class="seen">${icon("Check")}Watched</span>` : part ? `<span class="len now">${esc(leftText(durMs(e) - pos(e)))}</span>` : len ? `<span class="len">${esc(mins(len))}</span>` : ""}</span></button>`;
      }).join("")}</div></div></section>`;
  };
  // who made it, who scored it, and three of the cast, as words in columns under the story (the TV's credits). No photos.
  const credits = () => {
    const m = main.meta || {};
    const made = (isShow ? m.creator : m.director) || [], music = m.music || [], cast = (m.cast || []).slice(0, 3).map(p => p.name);
    const cols = [[isShow ? "Created by" : made.length > 1 ? "Directors" : "Director", made.slice(0, 2)], ["Music", music.slice(0, 1)], ["Starring", cast]].filter(c => c[1].length);
    if (!cols.length) return "";
    return `<div class="credits">${cols.map(([k, v]) => `<div><div class="caps">${k}</div><div class="names dots">${v.map(n => `<span>${esc(n)}</span>`).join("")}</div></div>`).join("")}</div>`;
  };
  const head = [displayYear(main) || "", ...(main.meta?.genres || []).slice(0, 2),
    !isShow && (durMs(main) || (main.meta?.runtime || 0) * 60000) ? hm(durMs(main) || main.meta.runtime * 60000) : "", main.meta?.cert].filter(Boolean).map(x => `<span>${esc(x)}</span>`).join("");
  const draw = () => {
    show(tabs("") + `<div class="tpage ${isShow ? "tshow" : "tfilm"}"><div class="stage">${art(main.meta?.backdrop)}<section class="hero ${main.meta?.backdrop ? "" : "bare"}"><div class="words">
        ${titleMark(main.meta?.logo, displayTitle(main))}
        <div class="facts">${head ? `<span class="dots fig">${head}</span>` : ""}${marksAfter((target || main).info)}</div>
        <div id="words">${words()}</div>${credits()}<div class="btns" id="btns">${buttons()}</div>
        ${target && !ready[target.id] ? `<p class="meta whynot">${icon("No")}<span>This browser can't show ${esc(blocked[target.id] || "this picture")}. A Light copy, added in Manager, would play here. Details lists where it does play.</span></p>` : ""}</div></section></div>
      <div id="below">${isShow ? episodes() : ""}</div></div>`);
    wire();
  };
  const wire = () => {
    wireButtons();
    const st = $("#pStory"); if (st) st.onclick = () => st.classList.toggle("open");
    for (const b of $$("[data-season]")) b.onclick = () => {
      season = +b.dataset.season;
      $("#below").innerHTML = episodes(); wire();
    };
    for (const c of $$("[data-ep]")) {
      const ep = seasons[season].episodes.find(e => e.id === c.dataset.ep);
      c.onclick = () => ready[ep.id] ? location.hash = `#/play/${ep.id}` : note(`This browser can't show ${blocked[ep.id] || "this episode's picture"}.`, true);
    }
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
    at("#pInfo", () => details(target || main, main));
  };
  draw();
}

// The numbers, one press away: a sheet over the page's blurred picture.
async function details(it, main) {
  const i = it.info || main.info || {};
  const how = d => !d.ok ? `No. It can't show ${d.why}` : d.how === "repack" ? "Yes, repackaged as it plays" : d.how === "light" ? "Yes, by its Light copy" : "Yes";
  const pic = img(main.meta?.backdrop, "w780");
  const fig = (v, n) => `<div><div class="title fig one">${esc(v)}</div><div class="caption one">${esc(n)}</div></div>`;
  const group = (h, rows) => rows.length ? `<div class="group"><div class="caps">${h}</div>${rows.map(([a, b]) => `<div class="spec"><span class="body">${esc(a)}</span><span class="label fig one" title="${esc(b)}">${esc(b)}</span></div>`).join("")}</div>` : "";
  const tracks = (i.tracks?.length ? i.tracks : i.audio ? [i.audio] : []).map(audioLine).sort((a, b) => (b[0] === "English") - (a[0] === "English"));
  const subs = [...(i.subs || []), ...(it.subs || []).map(s => s.label)].filter(Boolean);
  const langs = subs.length ? [...tracks, ["Subtitles", subs.some(s => /english/i.test(s)) ? "English" : "No English"]] : tracks;
  const picture = [i.w > 0 && i.h > 0 ? ["Resolution", `${i.w} × ${i.h}`] : null, i.vcodec ? ["Video", codecName(i.vcodec)] : null, i.hdr ? ["HDR", i.hdr] : null].filter(Boolean);
  const m = main.meta || {}, released = m.date && !isNaN(new Date(m.date)) ? new Date(m.date).toLocaleDateString(undefined, { day: "numeric", month: "long", year: "numeric" }) : "";
  const film = main.type === "movie" ? [released ? ["Released", released] : null, m.studio ? ["Studio", m.studio] : null].filter(Boolean)
    : main.type === "show" ? [released ? ["First shown", released] : null, m.studio ? ["Network", m.studio] : null,
      m.status ? ["Status", m.status.replace("Returning Series", "Still running")] : null].filter(Boolean) : [];
  const file = [it.size > 0 ? ["Size", gb(it.size)] : null, i.dur > 0 ? ["Length", lengthName(i.dur)] : null].filter(Boolean);
  const scrim = document.createElement("div"); scrim.className = "scrim";
  const sheetBg = document.createElement("div"); sheetBg.className = "scrim";
  sheetBg.style.background = "var(--base)"; sheetBg.style.overflow = "hidden";
  sheetBg.innerHTML = pic ? `<div class="sheetbg" style="background-image:url('${pic}')"></div>` : "";
  scrim.style.zIndex = 30;
  const s = document.createElement("aside"); s.className = "sheet"; s.tabIndex = -1;
  s.innerHTML = `<div class="head"><div style="min-width:0"><h2 class="title">Details</h2>${it.type === "episode" ? `<div class="meta mut one" style="margin-top:.4rem">${esc(epWords(it))}</div>` : ""}</div>
      <button class="tool" aria-label="Close" title="Close">${icon("Close")}</button></div>
    <div class="figures">${i.res ? fig(i.res, QUALITY[i.res] || i.res) : ""}${i.mbps > 0 ? fig(i.mbps.toFixed(1), "Mb/s") : ""}${i.fps > 0 ? fig(i.fps.toFixed(3).replace(".000", ""), "fps") : ""}</div>
    <div id="dPlays"></div>${group("Picture", picture)}${group("Languages", langs)}${group(main.type === "show" ? "Show" : "Film", film)}${group("File", file)}`;
  document.body.append(sheetBg, scrim, s);
  s.focus();
  const close = () => { off(); sheetBg.remove(); scrim.remove(); s.remove(); removeEventListener("keydown", onKey, true); $("#pInfo")?.focus(); };
  const off = onLeave(() => close());
  const onKey = e => {
    if (["Escape", "ArrowLeft", "ArrowRight", "Enter"].includes(e.key)) { e.preventDefault(); e.stopPropagation(); close(); }
  };
  addEventListener("keydown", onKey, true);
  scrim.onclick = close; $(".tool", s).onclick = close;
  // the sheet opens at once; where it plays is asked of mint and arrives a moment later
  if (it.id && (it.type === "movie" || it.type === "episode")) {
    const on = (await webAPI(`/api/v1/web/${it.id}`)).on, box = $("#dPlays", s);
    if (on && box) box.outerHTML = group("Plays on", [["This device", on.here], ["TV", on.tv], ["Phone", on.phone]].filter(r => r[1]).map(([k, d]) => [k, how(d)]));
  }
}

// Back to where you came from; Esc does the same on a title page, as Back does on the remote.
function goBack() { if (history.length > 1) history.back(); else location.hash = "#/"; }
document.addEventListener("keydown", e => {
  if (e.key === "Escape" && (route.key.startsWith("title") || route.key === "settings") && !$("#player") && !$(".scrim")) { e.preventDefault(); goBack(); }
});

// ---------- the keyboard: arrows move between things, as the remote does ----------
function focusables() {
  const sel = "a[href], button:not([disabled]):not([tabindex='-1']), input, textarea, select";
  return [...$$(sel, $("#topbar")), ...$$(sel, page())]
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
    case "settings": route.key = "settings"; settings(); break;
    case "films": case "shows": case "search": location.replace("#/"); break; // gone: everything is on Home, as on the TV
    case "title": route.key = "title" + parts[1]; title(parts[1]); break;
    case "manager": route.key = "manager"; Manager.page(); break;
    case "add": location.replace("#/manager"); break;
    default: location.hash = "#/";
  }
  requestAnimationFrame(() => { scrollTo(0, scrolls[h] || 0); barState(); });
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
