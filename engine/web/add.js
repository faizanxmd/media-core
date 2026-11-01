"use strict";
// Add: paste links, watch them download, look after the files, settings. It's a part of the site, not the site.
const Add = (() => {
  const ACTIVE = ["downloading", "processing", "converting", "paused", "queued"];
  const LIVE = ["downloading", "processing", "converting"];
  const VIDEO = /\.(mkv|mp4|m4v|avi|mov|ts|m2ts|wmv|webm)$/i;
  const AUDIO = /\.(flac|mp3|m4a|aac|ogg|opus|wav|alac|aiff?)$/i;
  const STATE = { downloading: "Downloading", processing: "Processing", converting: "Dolby Vision fix", queued: "Waiting", paused: "Paused", done: "Done", failed: "Failed", cancelled: "Cancelled" };

  function bytes(b) {
    if (b == null) return "Unknown size";
    const u = ["B", "KB", "MB", "GB", "TB"]; let i = 0;
    while (b >= 1000 && i < u.length - 1) { b /= 1000; i++; }
    return (i < 2 ? Math.round(b) : b.toFixed(b >= 100 ? 0 : 1)) + " " + u[i];
  }
  const speed = bps => (bps / 1e6).toFixed(bps >= 1e7 ? 0 : 1) + " MB/s";
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

  // a question: ask({title, text, input, buttons:[{label, value, cls}]}) -> {value, input}
  function ask({ title, text = "", input = false, buttons }) {
    return new Promise(res => {
      const scrim = document.createElement("div"); scrim.className = "scrim"; scrim.style.zIndex = 40;
      const d = document.createElement("div"); d.className = "ask";
      d.innerHTML = `<div class="title">${esc(title)}</div>${text ? `<p class="body mut" style="margin-top:.8rem">${esc(text)}</p>` : ""}
        ${input ? `<div class="fields" style="display:block"><input type="text" inputmode="url" placeholder="https://…" autocomplete="off" autocapitalize="off"></div>` : ""}
        <div class="btns">${buttons.map((b, i) => `<button class="word ${b.cls || ""}" data-i="${i}">${esc(b.label)}</button>`).join("")}</div>`;
      document.body.append(scrim, d);
      const inp = $("input", d);
      const done = value => { scrim.remove(); d.remove(); removeEventListener("keydown", key, true); res({ value, input: inp ? inp.value.trim() : "" }); };
      const key = e => { if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); done(""); } };
      addEventListener("keydown", key, true);
      scrim.onclick = () => done("");
      d.onclick = e => { const b = e.target.closest("[data-i]"); if (b) done(buttons[+b.dataset.i].value); };
      (inp || $(`[data-i="${buttons.length - 1}"]`, d)).focus();
    });
  }

  // ---------- live state ----------
  let S = null, online = false, tab = "active", es = null, pollT = 0, lastDone = null;
  const openJobs = new Set();

  function connect() {
    if (!window.EventSource) { poll(); return; }
    es?.close();
    es = new EventSource("/api/events");
    es.onmessage = e => { S = JSON.parse(e.data); online = true; changed(); };
    es.onerror = () => { online = false; changed(); };
  }
  async function poll() {
    const r = await api("/api/status");
    if (!r.error) { S = r; online = true; changed(); } else { online = false; changed(); }
    clearTimeout(pollT); pollT = setTimeout(poll, 3000);
  }
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible" && es && es.readyState === 2) connect();
  });

  // What the far end of the tabs says: "Add", or how a download is doing.
  function summary() {
    if (!S) return "";
    const live = S.jobs.filter(j => LIVE.includes(j.state));
    if (!live.length) return "";
    const d = live.find(j => j.state === "downloading");
    if (d) return `Downloading ${d.progress || 0}%`;
    const p = live[0];
    return (p.state === "converting" ? "Converting" : p.phase || "Processing") + (p.progress ? ` ${p.progress}%` : "");
  }
  function changed() {
    const t = $("#addTab");
    if (t && !t.classList.contains("on")) {
      const s = summary(), html = s ? `<i class="dot"></i>${esc(s)}` : "Add";
      if (t._h !== html) { t.innerHTML = html; t._h = html; }
    }
    if (route.key === "add") render();
  }

  // ---------- the page ----------
  function page() {
    show(tabs("Add") + `<div class="addpage">
      <form class="paste" id="drop"><textarea id="links" rows="1" placeholder="Paste links: one, or a whole list" aria-label="Download links" autocomplete="off" autocapitalize="off" spellcheck="false"></textarea>
        <button class="word" type="submit">Check</button></form>
      <div id="banner" class="warn" hidden></div>
      <section id="probes"></section>
      <nav class="subtabs">${[["active", "Downloads"], ["history", "Finished"], ["library", "Files"], ["settings", "Settings"]].map(([k, l]) => `<button class="kind" data-t="${k}">${l}<span id="n-${k}" class="mut"></span></button>`).join("")}
        <button class="kind right mut" id="clear" hidden>Clear finished</button></nav>
      <section id="jobs" aria-live="polite"></section><section id="library" hidden></section><section id="setbox" hidden></section></div>`);
    const ta = $("#links"), drop = $("#drop");
    const grow = () => { ta.style.height = "auto"; ta.style.height = ta.scrollHeight + "px"; };
    ta.addEventListener("input", grow);
    ta.addEventListener("keydown", e => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); check(grow); } });
    ta.addEventListener("paste", () => setTimeout(() => { grow(); if (urlsIn(ta.value).length) check(grow); }, 0));
    drop.addEventListener("submit", e => { e.preventDefault(); if (ta.value.trim()) check(grow); else ta.focus(); });
    drop.addEventListener("dragover", e => { e.preventDefault(); drop.classList.add("drag"); });
    drop.addEventListener("dragleave", () => drop.classList.remove("drag"));
    drop.addEventListener("drop", e => {
      e.preventDefault(); drop.classList.remove("drag");
      ta.value = e.dataTransfer.getData("text/uri-list") || e.dataTransfer.getData("text/plain"); grow(); check(grow);
    });
    for (const b of $$(".subtabs [data-t]")) b.onclick = () => { tab = b.dataset.t; if (tab === "library") loadLibrary(); render(); };
    $("#clear").onclick = async () => { const r = await api("/api/clear", {}); note(`Cleared ${r.removed || 0}`); };
    $("#jobs").addEventListener("click", jobClick);
    if (window.sharedLinks) { ta.value = window.sharedLinks; window.sharedLinks = ""; grow(); check(grow); }
    else if (!matchMedia("(pointer:coarse)").matches) ta.focus();
    if (tab === "library") loadLibrary();
    render();
  }

  function render() {
    if (!$("#jobs")) return;
    for (const b of $$(".subtabs [data-t]")) b.classList.toggle("on", b.dataset.t === tab);
    $("#jobs").hidden = tab !== "active" && tab !== "history";
    $("#library").hidden = tab !== "library";
    $("#setbox").hidden = tab !== "settings";
    if (tab === "settings") renderSettings();
    if (!S) {
      if (!online && tab !== "settings" && tab !== "library") $("#jobs").innerHTML = `<div class="empty">Connecting</div>`;
      return;
    }
    const b = $("#banner"), low = S.disk_total && S.disk_free < 20e9;
    b.hidden = S.aria2_ok && !low;
    b.textContent = !S.aria2_ok ? "The download engine (aria2) isn't answering. mint is restarting it; downloads resume on their own."
      : low ? `Only ${bytes(S.disk_free)} left on mint's disk. Files shows what's taking the space.` : "";
    const act = S.jobs.filter(j => ACTIVE.includes(j.state)), hist = S.jobs.filter(j => !ACTIVE.includes(j.state));
    $("#n-active").textContent = act.length ? ` ${act.length}` : "";
    $("#n-history").textContent = hist.length ? ` ${hist.length}` : "";
    $("#clear").hidden = tab !== "history" || !hist.some(j => j.state === "done" || j.state === "cancelled");
    const nDone = hist.filter(j => j.state === "done").length;
    if (tab === "library" && lastDone !== null && nDone !== lastDone) loadLibrary();
    lastDone = nDone;
    if (tab !== "active" && tab !== "history") return;
    const rank = j => LIVE.includes(j.state) ? 0 : j.state === "paused" ? 1 : 2;
    const list = tab === "active"
      ? act.sort((a, b) => rank(a) - rank(b) || (a.state === "queued" ? a.queue_pos - b.queue_pos : a.created - b.created))
      : hist.sort((a, b) => (b.finished || b.updated) - (a.finished || a.updated));
    const box = $("#jobs");
    if (!list.length) {
      box.innerHTML = `<div class="empty">${tab === "active" ? "Nothing downloading. Paste a link above and it lands in the library, named and sorted." : "Finished downloads show up here."}</div>`;
      return;
    }
    $(".empty", box)?.remove();
    const keep = new Set(list.map(j => j.id));
    for (const el of $$(".lrow", box)) if (!keep.has(el.dataset.id)) el.remove();
    let prev = null;
    for (const j of list) {
      let el = box.querySelector(`.lrow[data-id="${j.id}"]`);
      const html = jobHTML(j);
      if (!el) { el = document.createElement("article"); el.dataset.id = j.id; el.className = "lrow wrap"; }
      if (el._html !== html) { el.innerHTML = html; el._html = html; }
      const want = prev ? prev.nextSibling : box.firstChild;
      if (el !== want) box.insertBefore(el, want);
      prev = el;
    }
  }

  function planTitle(p, fallback) {
    p = p || {};
    if (p.kind === "movie" && p.title) return p.title + (p.year ? ` (${p.year})` : "");
    if (p.kind === "tv" && p.title) return p.title + " · " + (p.episode !== "" && p.episode != null ? `S${pad2(p.season || 1)}E${pad2(p.episode)}` : `Season ${Number(p.season) || 1}`);
    if (p.kind === "music") return p.title || "Music";
    return fallback || "Download";
  }

  function jobHTML(j) {
    const total = j.total_bytes || j.size;
    const name = planTitle(j.plan, (j.dest || "").split("/").pop() || j.filename);
    const sub = [j.state === "processing" && j.phase ? j.phase : STATE[j.state] || j.state];
    let right = "", bar = "";
    if (j.state === "downloading" || j.state === "paused" || j.state === "queued") {
      bar = `<div class="jobbar"><i style="width:${j.progress || 0}%"></i></div>`;
      right = j.done_bytes ? `${j.progress}%` : bytes(total);
      if (j.done_bytes) sub.push(`${bytes(j.done_bytes)} of ${bytes(total)}`);
      if (j.state === "downloading") {
        if (j.speed_bps) sub.push(speed(j.speed_bps));
        if (j.eta_s) sub.push(`${dur(j.eta_s)} left`);
      } else if (j.state === "queued" && total) {
        const s = S.settings.avg_bps > 1e5 ? S.settings.avg_bps : 3.5e6;
        sub.push(`about ${dur((total - (j.done_bytes || 0)) / s)}`);
      }
    } else if (j.state === "processing" || j.state === "converting") {
      bar = `<div class="jobbar"><i style="width:${j.progress || 0}%"></i></div>`;
      if (j.state === "converting" && j.phase) sub.push(j.phase.replace(/^Dolby Vision 7 → 8\.1: /, ""));
      right = j.progress ? `${j.progress}%` : "";
    } else {
      right = bytes(total);
      if (j.finished || j.updated) sub.push(ago(j.finished || j.updated));
    }
    const b = (act, ic, label) => `<button class="tool" data-act="${act}" title="${label}" aria-label="${label}">${icon(ic)}</button>`;
    const acts = [];
    if (j.state === "downloading") acts.push(b("pause", "Pause", "Pause"));
    if (j.state === "queued") acts.push(b("top", "SkipNext", "Download next"), b("pause", "Pause", "Pause"));
    if (j.state === "paused") acts.push(b("resume", "Play", "Resume"));
    if (["failed", "cancelled"].includes(j.state)) acts.push(b("retry", "Retry", "Try again"), b("newlink", "Link", "Use a fresh link"));
    acts.push(ACTIVE.includes(j.state) ? b("cancel", "Close", "Cancel") : b("remove", "Close", "Remove from history"));
    const msg = ["failed", "cancelled"].includes(j.state) && j.message ? `<div class="warn more">${esc(j.message)}</div>` : "";
    const more = openJobs.has(j.id) ? `<div class="more"><div>File: ${esc(j.filename)}</div>${j.dest ? `<div>Into: ${esc(j.dest)}</div>` : ""}<div>From: ${esc(j.url)}</div>
        <pre>${esc((j.log || []).join("\n"))}</pre></div>` : "";
    return `<div class="main"><div class="label one title1">${esc(name)}</div><div class="meta sub one">${sub.map(esc).join(" · ")}</div></div>
      ${right ? `<div class="meta size fig">${right}</div>` : ""}<div class="acts">${acts.join("")}</div>${msg}${more}${bar}`;
  }

  async function jobClick(e) {
    const row = e.target.closest(".lrow"); if (!row || e.target.closest(".more")) return;
    const id = row.dataset.id, btn = e.target.closest("button[data-act]");
    if (!btn) { openJobs.has(id) ? openJobs.delete(id) : openJobs.add(id); render(); return; }
    const j = S.jobs.find(x => x.id === id);
    let act = btn.dataset.act, body = {};
    if (act === "cancel") {
      const r = await ask({ title: "Cancel this download?", text: j.state === "processing" || j.state === "converting"
        ? "This stops processing. Try again later picks it up where it left off." : "Keep the partial file to resume later, or delete it.", buttons: [
        { label: "Cancel, keep the partial file", value: "keep", cls: "danger" },
        ...(LIVE.includes(j.state) && j.state !== "downloading" ? [] : [{ label: "Cancel and delete it", value: "delete", cls: "danger" }]),
        { label: "Keep going", value: "" }] });
      if (!r.value) return;
      body = { delete: r.value === "delete" };
    }
    if (act === "newlink") {
      const r = await ask({ title: "A fresh link for the same file", text: "It resumes where it stopped. The size must match.", input: true,
        buttons: [{ label: "Back", value: "" }, { label: "Resume with this link", value: "ok" }] });
      if (!r.value || !r.input) return;
      act = "retry"; body = { url: r.input };
      note("Checking the new link…");
    }
    btn.disabled = true;
    const r = await api(`/api/jobs/${id}/${act}`, body);
    btn.disabled = false;
    if (r.error) note(r.error);
    if (!es || es.readyState !== 1) poll();
  }

  // ---------- links ----------
  const urlsIn = text => [...new Set((text.match(/https?:\/\/[^\s<>"'`]+/g) || []).map(u => u.replace(/[.,;)\]>'"]+$/, "")))];
  async function check(grow) {
    const ta = $("#links"), urls = urlsIn(ta.value);
    if (!urls.length) { if (ta.value.trim()) note("No http(s) link in there"); return; }
    ta.value = ""; grow(); ta.blur();
    const box = $("#probes");
    const cards = urls.map(u => {
      const el = document.createElement("div");
      el.className = "panel"; el.innerHTML = loadingHTML(u);
      box.append(el);
      return [u, el];
    });
    allBar();
    let i = 0;
    const worker = async () => { while (i < cards.length) { const [u, el] = cards[i++]; await probeInto(u, el); } };
    await Promise.all([worker(), worker(), worker()]);
  }
  const loadingHTML = u => `<div class="facts" style="margin:0"><div class="loading s"></div><span class="one">Checking ${esc(u.length > 90 ? u.slice(0, 90) + "…" : u)}</span></div>`;

  async function probeInto(url, el) {
    const p = await api("/api/probe", { url });
    if (!p.ok) {
      el.innerHTML = `<div class="label">That link didn't work</div><div class="meta file">${esc(url)}</div><div class="warn">${esc(p.error)}</div>
        <div class="btns"><button class="word" data-x>Dismiss</button><button class="word" data-again>Try again</button></div>`;
      $("[data-x]", el).onclick = () => { el.remove(); allBar(); };
      $("[data-again]", el).onclick = () => { el.innerHTML = loadingHTML(url); probeInto(url, el); };
      allBar();
      return;
    }
    el._probe = p;
    const pl = p.plan;
    const facts = [bytes(p.size), p.resumable ? "Resumes" : "Can't resume", plural(p.connections, "connection")];
    if (p.eta_min != null) facts.push(`about ${dur(p.eta_min * 60)}`);
    facts.push(p.host);
    el.innerHTML = `<div class="heading" data-title></div><div class="meta file">${esc(p.filename)}</div>
      <div class="facts meta fig">${facts.map(esc).join(" · ")}</div>
      ${(p.warnings || []).map(w => `<div class="warn">${esc(w)}</div>`).join("")}
      <div class="kinds" data-kind><button type="button" class="kind" data-v="movie">Film</button><button type="button" class="kind" data-v="tv">Show</button><button type="button" class="kind" data-v="music">Music</button></div>
      <div class="fields">
        <label class="c4" data-f="title"><span></span><input name="title" autocomplete="off"></label>
        <label class="c2" data-f="year">Year<input name="year" inputmode="numeric" autocomplete="off"></label>
        <label class="c3" data-f="season">Season<input name="season" inputmode="numeric" autocomplete="off"></label>
        <label class="c3" data-f="episode">Episode (blank for a whole season)<input name="episode" inputmode="numeric" autocomplete="off"></label>
      </div><div class="dest" data-dest></div>
      <div class="btns"><button class="word" data-x>Dismiss</button><button class="play" data-go>Download</button></div>`;
    for (const k of ["title", "year", "season", "episode"]) {
      const inp = $(`input[name=${k}]`, el);
      inp.value = pl[k] ?? "";
      if (k === "title" && pl.kind === "music") inp.value = "";
      inp.oninput = () => refreshPlan(el);
    }
    el._kind = pl.kind;
    for (const b of $$("[data-kind] button", el)) b.onclick = () => {
      if (b.dataset.v === "music" && el._kind !== "music") $("input[name=title]", el).value = "";
      el._kind = b.dataset.v; refreshPlan(el);
    };
    $("[data-x]", el).onclick = () => { el.remove(); allBar(); };
    $("[data-go]", el).onclick = () => queue(el);
    refreshPlan(el); allBar();
  }
  function planOf(el) {
    const v = n => $(`input[name=${n}]`, el).value.trim();
    return { ...el._probe.plan, kind: el._kind, title: v("title"), year: v("year"), season: v("season"), episode: v("episode") };
  }
  function refreshPlan(el) {
    const k = el._kind;
    for (const b of $$("[data-kind] button", el)) b.classList.toggle("on", b.dataset.v === k);
    $("[data-f=year]", el).hidden = k !== "movie";
    $("[data-f=season]", el).hidden = $("[data-f=episode]", el).hidden = k !== "tv";
    $("[data-f=title]", el).className = k === "movie" ? "c4" : "c6";
    $("[data-f=title] span", el).textContent = k === "music" ? "Artist (blank to read it from the tags)" : k === "tv" ? "Show" : "Title";
    $("[data-title]", el).textContent = planTitle(planOf(el), el._probe.filename);
    clearTimeout(el._t);
    el._t = setTimeout(async () => {
      const r = await api("/api/plan", { plan: planOf(el) });
      $("[data-dest]", el).textContent = r.error ? r.error : r.dest + (r.note ? ` (${r.note})` : "");
    }, 200);
  }
  async function queue(el) {
    const go = $("[data-go]", el);
    go.disabled = true; go.textContent = "Queuing…";
    const r = await api("/api/jobs", { probe: el._probe, plan: planOf(el) });
    if (r.error) { note(r.error); go.disabled = false; go.textContent = "Download"; return false; }
    el.remove(); allBar();
    note("Queued: " + (r.dest || planOf(el).title || el._probe.filename));
    tab = "active"; render();
    return true;
  }
  function allBar() {
    const box = $("#probes"); if (!box) return;
    $(".allbar", box)?.remove();
    const ready = $$(".panel", box).filter(el => el._probe);
    if (ready.length < 2) return;
    const bar = document.createElement("div");
    bar.className = "btns allbar"; bar.style.justifyContent = "flex-end";
    bar.innerHTML = `<button class="play">Download all ${ready.length}</button>`;
    $("button", bar).onclick = async () => { for (const el of ready) if (!(await queue(el))) break; };
    box.append(bar);
  }

  // ---------- the files on mint's disk ----------
  let LIB = null, sec = "Movies", ITEMS = [], query = "", sort = "added", detail = null;
  async function loadLibrary() {
    const r = await api("/api/library");
    if (!$("#library")) return;
    if (r.error) { if (!LIB) $("#library").innerHTML = `<div class="empty">${esc(r.error)}</div>`; return; }
    LIB = r; drawLibrary();
    if (detail) { const it = libItems().find(x => x.node.path === detail.node.path); if (it) openItem(it); else closeItem(); }
  }
  const count = (n, re) => n.dir ? (n.children || []).reduce((a, c) => a + count(c, re), 0) : re.test(n.name) ? 1 : 0;
  const files = n => n.dir ? (n.children || []).flatMap(files) : [n];
  function libItems() {
    const s = LIB?.[sec]; if (!s) return [];
    const out = [], name = n => n.dir ? n.name : n.name.replace(/\.[^.]+$/, "");
    for (const n of s.children || []) {
      if (sec === "Music") {
        const albums = (n.children || []).filter(c => c.dir);
        for (const a of albums) out.push({ node: a, kind: "album", meta: a.meta, artist: n.meta?.title || n.name, title: a.meta?.title || a.name, year: a.meta?.year });
        if (!albums.length || n.children.some(c => !c.dir && AUDIO.test(c.name)))
          out.push({ node: n, kind: "album", meta: n.meta, artist: n.meta?.title || n.name, title: albums.length ? "Singles" : name(n), loose: !!albums.length });
        continue;
      }
      const m = name(n).match(/^(.*?)\s*\((\d{4})\)$/);
      out.push({ node: n, kind: sec === "Movies" ? "film" : "show", meta: n.meta, title: n.meta?.title || (m ? m[1] : name(n)), year: n.meta?.year || (m ? m[2] : "") });
    }
    return out;
  }
  function drawLibrary() {
    const box = $("#library"); if (!box || !LIB) return;
    if (!$("#libList", box)) {
      box.innerHTML = `<div class="libbar"><div class="kinds">${[["Movies", "Films"], ["TV Shows", "Shows"], ["Music", "Music"]].map(([v, l]) => `<button class="kind" data-sec="${v}">${l}</button>`).join("")}</div>
          <label class="query">${icon("Search")}<input id="libQ" type="search" placeholder="Search" autocomplete="off" spellcheck="false"></label>
          <select id="libSort" aria-label="Sort"><option value="added">Newest first</option><option value="size">Biggest first</option><option value="name">A–Z</option></select></div>
        <div class="sumline meta"><span id="libSum" class="fig"></span><span id="libDisk" class="fig"></span></div><div id="libList"></div>`;
      for (const b of $$("[data-sec]", box)) b.onclick = () => { sec = b.dataset.sec; drawLibrary(); };
      $("#libQ").oninput = e => { query = e.target.value.trim().toLowerCase(); drawLibrary(); };
      $("#libSort").onchange = e => { sort = e.target.value; drawLibrary(); };
      $("#libList").onclick = e => {
        const d = e.target.closest("[data-del]");
        if (d) { e.stopPropagation(); remove(d); return; }
        const r = e.target.closest("[data-i]");
        if (r) openItem(ITEMS[+r.dataset.i]);
      };
    }
    for (const b of $$("[data-sec]", box)) b.classList.toggle("on", b.dataset.sec === sec);
    const all = libItems(), hay = it => [it.title, it.artist, it.node.name].join(" ").toLowerCase();
    ITEMS = all.filter(it => !query || hay(it).includes(query)).sort((a, b) =>
      sort === "size" ? b.node.size - a.node.size : sort === "name" ? a.title.localeCompare(b.title) : b.node.added - a.node.added);
    const noun = { Movies: "film", "TV Shows": "show", Music: "album" }[sec];
    $("#libSum").textContent = `${plural(all.length, noun)} · ${bytes(LIB[sec]?.size || 0)}`;
    $("#libDisk").textContent = `${bytes(LIB.disk_free)} free`;
    $("#libList").innerHTML = !ITEMS.length ? `<div class="empty">${query ? `Nothing here matches “${esc(query)}”.` : "Downloads land here once they're done."}</div>`
      : ITEMS.map((it, i) => `<div class="lrow" data-i="${i}"><div class="thumb">${it.meta?.thumb ? `<img src="${esc(it.meta.thumb)}" alt="" loading="lazy">` : ""}</div>
          <div class="main"><div class="label one title1">${esc(it.title)}${it.year && it.kind !== "album" ? ` <span class="mut" style="font-weight:500">${esc(it.year)}</span>` : ""}</div>
          <div class="meta sub one">${esc(subline(it))}</div></div>
          <div class="meta size fig">${bytes(it.node.size)}</div><div class="acts">${delBtn(it.node, it.title)}</div></div>`).join("");
  }
  function subline(it) {
    const n = it.node, s = [];
    if (it.kind === "show") {
      const seasons = (n.children || []).filter(c => c.dir).length, eps = count(n, VIDEO);
      if (seasons) s.push(plural(seasons, "season")); if (eps) s.push(plural(eps, "episode"));
    }
    if (it.kind === "album") { s.push(it.artist); if (it.year) s.push(it.year); s.push(plural(count(n, AUDIO), "track")); }
    if (n.added && it.kind !== "album") s.push("added " + ago(n.added));
    return s.join(" · ");
  }
  const delBtn = (n, label = n.name) => `<button class="tool" data-del="${esc(n.path)}" data-name="${esc(label)}" data-size="${n.size}" data-files="${n.files}" title="Delete" aria-label="Delete ${esc(label)}">${icon("Delete")}</button>`;
  async function remove(b) {
    const { del: path, name } = b.dataset, size = +b.dataset.size, n = +b.dataset.files;
    const r = await ask({ title: `Delete “${name}”?`, text: `This permanently removes ${bytes(size)} (${plural(n, "file")}) from mint's disk. There's no undo.`,
      buttons: [{ label: `Delete ${bytes(size)}`, value: "del", cls: "danger" }, { label: "Keep it", value: "" }] });
    if (r.value !== "del") return;
    b.disabled = true;
    const res = await api("/api/library/delete", { path });
    if (res.error) { b.disabled = false; note(res.error); return; }
    note(`Deleted ${name}, ${bytes(res.freed)} freed`);
    loadLibrary();
  }
  function closeItem() { detail = null; $("#itemSheet")?.remove(); $("#itemScrim")?.remove(); removeEventListener("keydown", itemKey, true); }
  const itemKey = e => { if (e.key === "Escape" && !$(".ask")) { e.preventDefault(); e.stopPropagation(); closeItem(); } };
  function openItem(it) {
    detail = it;
    let s = $("#itemSheet");
    if (!s) {
      const scrim = document.createElement("div"); scrim.className = "scrim"; scrim.id = "itemScrim"; scrim.onclick = closeItem;
      s = document.createElement("aside"); s.className = "sheet"; s.id = "itemSheet"; s.tabIndex = -1;
      document.body.append(scrim, s);
      addEventListener("keydown", itemKey, true);
      s.addEventListener("click", e => {
        if (e.target.closest("[data-close]")) { closeItem(); return; }
        const b = e.target.closest("[data-del]"); if (b) remove(b);
      });
      s.focus();
    }
    const n = it.node, tree = (nodes) => nodes.map(f => `<div class="frow"><span class="name meta">${esc(f.meta?.title && VIDEO.test(f.name) ? f.meta.title : f.name)}</span><span class="meta size fig">${bytes(f.size)}</span>${delBtn(f, f.name)}</div>`).join("");
    let body = "";
    if (it.kind === "show") {
      for (const c of (n.children || []).filter(c => c.dir)) body += `<div class="group"><div class="head"><div class="caption">${esc(c.name)} · ${bytes(c.size)}</div>${delBtn(c, c.name)}</div>${tree(files(c))}</div>`;
      const loose = (n.children || []).filter(c => !c.dir);
      if (loose.length) body += `<div class="group"><div class="caption">Other files</div>${tree(loose)}</div>`;
    } else body = `<div class="group"><div class="caption">Files</div>${tree(it.loose ? (n.children || []).filter(c => !c.dir) : files(n))}</div>`;
    s.innerHTML = `<div class="head"><div style="min-width:0"><h2 class="title">${esc(it.title)}</h2><div class="meta mut" style="margin-top:.4rem">${plural(n.files, "file")} · ${bytes(n.size)}${n.added ? ` · added ${ago(n.added)}` : ""}</div></div>
        <button class="tool" data-close aria-label="Close" title="Close">${icon("Close")}</button></div>${body}
      ${it.loose ? "" : `<div class="btns" style="justify-content:flex-end;margin-top:2.4rem"><button class="word danger" data-del="${esc(n.path)}" data-name="${esc(it.title)}" data-size="${n.size}" data-files="${n.files}">Delete all</button></div>`}`;
  }

  // ---------- settings ----------
  function renderSettings() {
    const box = $("#setbox"); if (!box) return;
    if (!S) { box.innerHTML = `<div class="empty">Connecting</div>`; return; }
    const s = S.settings;
    const seg = (key, vals) => `<div class="seg" data-key="${key}">${vals.map(([v, l]) => `<button data-v="${v}" class="${s[key] === v ? "on" : ""}">${l}</button>`).join("")}</div>`;
    const html = `<div class="setrow"><div>Download limit<small>MB/s</small></div>${seg("limit_mbps", [[0, "Off"], [1, "1"], [2, "2"], [3, "3"], [5, "5"]])}</div>
      <div class="setrow"><div>Downloads at once</div>${seg("max_concurrent", [[1, "1"], [2, "2"], [3, "3"]])}</div>
      <div class="setrow"><div>Media Core for the TV<small>It updates itself after this.</small></div><a class="word" href="/api/v1/app.apk" download="media-core.apk">Get APK</a></div>
      <div class="setrow"><div>Media Core for the phone<small>It updates itself after this.</small></div><a class="word" href="/api/v1/phone-app.apk" download="media-core-phone.apk">Get APK</a></div>`;
    if (box._h === html) return;
    box._h = html; box.innerHTML = html;
    for (const o of $$(".seg", box)) for (const b of $$("button", o)) b.onclick = async () => {
      const val = +b.dataset.v;
      const r = await api("/api/settings", { [o.dataset.key]: val });
      if (r.error) note(r.error); else { S.settings = r; box._h = ""; renderSettings(); note("Saved"); }
    };
  }

  return { connect, page, summary };
})();
window.Add = Add;
