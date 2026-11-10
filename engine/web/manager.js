"use strict";
// Manager: paste a link and see what it is before it downloads, the downloads themselves, the library's files with
// where each one plays, and how mint is doing. Words and sizes follow docs/design-system.md.
const Manager = (() => {
  const ACTIVE = ["downloading", "processing", "converting", "paused", "queued"];
  const LIVE = ["downloading", "processing", "converting"];
  const AUDIO = /\.(flac|mp3|m4a|aac|ogg|opus|wav|alac|aiff?)$/i;

  // ---------- words ----------
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
    if (s >= 3600) return `${Math.floor(s / 3600)} h ${Math.floor(s % 3600 / 60)} min`;
    if (s >= 60) return `${Math.round(s / 60)} min`;
    return `${s} s`;
  }
  function ago(t) {
    const d = new Date(t * 1000), now = new Date(), s = (now - d) / 1000;
    const hm = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
    if (s < 60) return "just now";
    if (d.toDateString() === now.toDateString()) return `Today, ${hm}`;
    if (s < 2 * 86400 && new Date(now - 86400).toDateString() === d.toDateString()) return `Yesterday, ${hm}`;
    if (s < 7 * 86400) return `${Math.round(s / 86400)} days ago`;
    return d.toLocaleDateString(undefined, { day: "numeric", month: "short" });
  }
  const plural = (n, one, many = one + "s") => `${n} ${n === 1 ? one : many}`;
  const pad2 = n => String(Number(n) || 0).padStart(2, "0");
  const quality = m => !m ? "" : [m.res, m.hdr ? m.hdr.replace(/ \d.*$/, "") : ""].filter(Boolean).join(" ");
  const videoWord = m => !m?.vcodec ? "" : m.vcodec.replace(" Main 10", "") + (m.vbits >= 10 ? " 10-bit" : "");
  const box = m => (m?.container || "").toUpperCase();
  // where a file plays, for the browser that's asking (the engine decides: play.go)
  const plays = f => f.play === "light" ? "Browsers by its Light copy" : f.browser ? "Plays everywhere" : "TV and phone only";
  const playsIn = f => ({ direct: "Browsers, TV and phone", repack: "Browsers (repackaged as it plays), TV and phone", light: "Browsers (by the Light copy), TV and phone" })[f.play]
    || "TV and phone. This browser can't decode its picture; a Light copy would play here";
  const probeLink = async url => api(`/api/probe?can=${await browserCan}`, { url });
  const facts = (...rows) => `<div class="mfacts">${rows.filter(r => r[1]).map(([k, v]) => `<div><div class="caption mut">${k}</div><div class="label fig">${esc(v)}</div></div>`).join("")}</div>`;
  const mediaFacts = (m, size) => facts(["Quality", quality(m)], ["Video", videoWord(m)], ["File", box(m)], ["Audio", m?.audio || ""],
    ["Length", m?.dur ? dur(m.dur) : ""], ["Size", size != null ? bytes(size) : ""]);
  const downloadTime = sizeB => sizeB && S?.settings ? dur(sizeB / (S.settings.avg_bps > 1e5 ? S.settings.avg_bps : 3.5e6)) : "";

  // a question: ask({title, text, input, buttons:[{label, value, cls}]}) -> {value, input}
  function ask({ title, text = "", html = "", input = false, buttons }) {
    return new Promise(res => {
      const scrim = document.createElement("div"); scrim.className = "scrim"; scrim.style.zIndex = 40;
      const d = document.createElement("div"); d.className = "ask";
      d.innerHTML = `<div class="title">${esc(title)}</div>${text ? `<p class="body mut" style="margin-top:.8rem">${esc(text)}</p>` : ""}${html}
        ${input ? `<input class="mlink" type="text" inputmode="url" placeholder="https://…" autocomplete="off" autocapitalize="off" spellcheck="false">` : ""}
        <div class="btns">${buttons.map((b, i) => `<button class="word ${b.cls || ""}" data-i="${i}">${esc(b.label)}</button>`).join("")}</div>`;
      document.body.append(scrim, d);
      const inp = $("input", d);
      const done = value => { scrim.remove(); d.remove(); removeEventListener("keydown", key, true); res({ value, input: inp ? inp.value.trim() : "" }); };
      const key = e => { if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); done(""); } if (e.key === "Enter" && inp && document.activeElement === inp) { e.preventDefault(); done(buttons[buttons.length - 1].value); } };
      addEventListener("keydown", key, true);
      scrim.onclick = () => done("");
      d.onclick = e => { const b = e.target.closest("[data-i]"); if (b) done(buttons[+b.dataset.i].value); };
      (inp || $(`[data-i="${buttons.length - 1}"]`, d)).focus();
    });
  }

  // ---------- live state ----------
  let S = null, online = false, tab = "downloads", es = null, pollT = 0, lastDone = null;
  let TITLES = null, SYS = null, LIB = null, sysT = 0;
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
    if (!r.error) { S = r; online = true; } else online = false;
    changed();
    clearTimeout(pollT); pollT = setTimeout(poll, 3000);
  }
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible" && es && es.readyState === 2) connect();
  });

  // something is downloading or being worked on: the tab carries a dot
  const busy = () => !!S && S.jobs.some(j => LIVE.includes(j.state));
  function changed() {
    const t = $("#mgrTab");
    if (t) { const on = busy(); if (t._b !== on) { t._b = on; t.innerHTML = (on ? `<i class="dot"></i>` : "") + "Manager"; } }
    if (route.key !== "manager") return;
    const nDone = S ? S.jobs.filter(j => j.state === "done").length : 0;
    if (lastDone !== null && nDone !== lastDone) loadTitles();
    lastDone = nDone;
    render();
  }

  async function loadTitles() {
    const r = await webAPI("/api/manager/titles");
    if (!r.error) { TITLES = r.titles || []; render(); }
  }
  async function loadSystem() {
    const r = await api("/api/system");
    if (!r.error) { SYS = r; if (tab === "system") render(); }
  }

  // ---------- the page ----------
  function page() {
    show(tabs("Manager") + `<div class="addpage">
      <form class="paste" id="drop"><textarea id="links" rows="1" placeholder="Paste a link" aria-label="Download links" autocomplete="off" autocapitalize="off" spellcheck="false"></textarea></form>
      <div id="banner" class="warn" hidden></div>
      <section id="probes"></section>
      <nav class="subtabs">${[["downloads", "Downloads"], ["library", "Library"], ["system", "System"]].map(([k, l]) => `<button class="kind" data-t="${k}">${l}<span id="n-${k}" class="mut fig"></span></button>`).join("")}</nav>
      <section id="body" aria-live="polite"></section></div>`);
    const ta = $("#links"), drop = $("#drop");
    const grow = () => { ta.style.height = "auto"; ta.style.height = ta.scrollHeight + "px"; };
    ta.addEventListener("input", grow);
    ta.addEventListener("keydown", e => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); check(grow); } });
    ta.addEventListener("paste", () => setTimeout(() => { grow(); if (urlsIn(ta.value).length) check(grow); }, 0));
    drop.addEventListener("submit", e => { e.preventDefault(); check(grow); });
    drop.addEventListener("dragover", e => { e.preventDefault(); drop.classList.add("drag"); });
    drop.addEventListener("dragleave", () => drop.classList.remove("drag"));
    drop.addEventListener("drop", e => {
      e.preventDefault(); drop.classList.remove("drag");
      ta.value = e.dataTransfer.getData("text/uri-list") || e.dataTransfer.getData("text/plain"); grow(); check(grow);
    });
    for (const b of $$(".subtabs [data-t]")) b.onclick = () => { tab = b.dataset.t; $("#body").innerHTML = ""; $("#body")._k = ""; enter(); render(); };
    $("#body").addEventListener("click", bodyClick);
    $("#body").addEventListener("input", e => { if (e.target.id === "libQ") { query = e.target.value.trim().toLowerCase(); drawLibrary(); } });
    $("#body").addEventListener("change", e => { if (e.target.id === "libSort") { sort = e.target.value; drawLibrary(); } });
    if (window.sharedLinks) { ta.value = window.sharedLinks; window.sharedLinks = ""; grow(); check(grow); }
    else if (!matchMedia("(pointer:coarse)").matches) ta.focus();
    loadTitles(); enter(); render();
  }
  function enter() {
    clearInterval(sysT);
    if (tab === "system") { loadSystem(); sysT = setInterval(() => route.key === "manager" && tab === "system" ? loadSystem() : clearInterval(sysT), 10000); }
    if (tab === "library" && sec === "music") loadMusic();
  }

  function render() {
    const body = $("#body"); if (!body) return;
    for (const b of $$(".subtabs [data-t]")) b.classList.toggle("on", b.dataset.t === tab);
    const act = S ? S.jobs.filter(j => ACTIVE.includes(j.state)) : [];
    $("#n-downloads").textContent = act.length ? ` ${act.length}` : "";
    const ban = $("#banner"), low = S?.disk_total && S.disk_free < 20e9;
    ban.hidden = !S || S.aria2_ok && !low;
    ban.textContent = !S ? "" : !S.aria2_ok ? "The downloader (aria2) isn't answering. mint is restarting it, and downloads carry on by themselves."
      : low ? `Only ${bytes(S.disk_free)} left on the library disk. Library, biggest first, shows what's taking the space.` : "";
    if (tab === "downloads") drawJobs(body, act);
    else if (tab === "library") drawLibrary();
    else drawSystem();
  }

  // ---------- downloads ----------
  function planTitle(p, fallback) {
    p = p || {};
    let t = fallback || "Download";
    if (p.kind === "convert") t = p.title || fallback;
    else if (p.kind === "movie" && p.title) t = p.title + (p.year ? ` (${p.year})` : "");
    else if (p.kind === "tv" && p.title) t = p.title + " · " + (p.episode !== "" && p.episode != null ? `S${pad2(p.season || 1)}E${pad2(p.episode)}` : `Season ${Number(p.season) || 1}`);
    else if (p.kind === "music") t = p.title || "Music";
    if (p.light) t += " · Light copy";
    if (p.kind === "convert") t += " · Make MP4";
    return t;
  }
  function posterFor(j) {
    if (!TITLES) return "";
    const dest = j.plan?.kind === "convert" ? j.plan.path : j.plan?.light ? j.plan.into : j.dest || "";
    const t = TITLES.find(t => t.poster && dest && (dest === t.path || dest.startsWith(t.path + "/")));
    return t ? img(t.poster, "w185") : "";
  }
  function drawJobs(body, act) {
    if (!S) { body.innerHTML = `<div class="empty">${online ? "" : "Connecting"}</div>`; return; }
    const rank = j => LIVE.includes(j.state) ? 0 : j.state === "paused" ? 1 : 2;
    const list = act.sort((a, b) => rank(a) - rank(b) || (a.state === "queued" ? a.queue_pos - b.queue_pos : a.created - b.created));
    const hist = S.jobs.filter(j => !ACTIVE.includes(j.state)).sort((a, b) => (b.finished || b.updated) - (a.finished || a.updated));
    if (body._k !== "jobs") { body._k = "jobs"; body.innerHTML = `<div id="active"></div><div id="hist"></div>`; }
    const fill = (el, jobs) => {
      $(".empty", el)?.remove();
      const keep = new Set(jobs.map(j => j.id));
      for (const r of $$(".lrow", el)) if (!keep.has(r.dataset.id)) r.remove();
      let prev = null;
      for (const j of jobs) {
        let r = el.querySelector(`.lrow[data-id="${j.id}"]`);
        const html = jobHTML(j);
        if (!r) { r = document.createElement("article"); r.dataset.id = j.id; r.className = "lrow wrap"; }
        if (r._html !== html) { r.innerHTML = html; r._html = html; }
        const want = prev ? prev.nextSibling : el.querySelector(".lrow");
        if (r !== want) el.insertBefore(r, want);
        prev = r;
      }
    };
    const a = $("#active"), h = $("#hist");
    if (!list.length) { a.innerHTML = `<div class="empty">Nothing downloading. Paste a link above: it lands in the library, named and sorted.</div>`; }
    else fill(a, list);
    if (!hist.length) h.innerHTML = "";
    else {
      if (!$(".msec", h)) h.innerHTML = `<div class="msec"><span class="caption mut">Finished</span><button class="kind mut" data-clear>Clear</button></div>`;
      fill(h, hist);
    }
  }
  function jobHTML(j) {
    const total = j.total_bytes || j.size;
    const name = planTitle(j.plan, (j.dest || "").split("/").pop() || j.filename);
    let sub = [], right = "", bar = "";
    if (j.state === "downloading") {
      sub.push("Downloading");
      if (j.speed_bps) sub.push(speed(j.speed_bps));
      if (j.eta_s) sub.push(`${dur(j.eta_s)} left`);
      right = j.done_bytes ? `${bytes(j.done_bytes)} of ${bytes(total)}` : bytes(total);
      bar = j.progress || 0;
    } else if (j.state === "queued" || j.state === "paused") {
      sub.push(j.state === "paused" ? "Paused" : j.plan?.light ? "Waiting · starts after the film" : "Waiting");
      if (j.state === "queued" && total && !j.plan?.light) sub.push(`about ${downloadTime(total - (j.done_bytes || 0))}`);
      right = j.done_bytes ? `${j.progress}%` : bytes(total);
      if (j.done_bytes) bar = j.progress || 0;
    } else if (j.state === "processing" || j.state === "converting") {
      sub.push(j.state === "converting" ? "Dolby Vision fix" : j.phase || "Processing");
      right = j.progress ? `${j.progress}%` : "";
      if (j.progress) bar = j.progress;
    } else {
      sub.push(j.state === "done" ? ago(j.finished || j.updated) : j.state === "failed" ? "Failed" : "Cancelled");
      right = j.state === "done" ? bytes(total) : "";
    }
    const b = (act, ic, label) => `<button class="tool" data-act="${act}" title="${label}" aria-label="${label}">${icon(ic)}</button>`;
    const acts = [];
    if (j.state === "downloading") acts.push(b("pause", "Pause", "Pause"));
    if (j.state === "queued" && j.gid) acts.push(b("top", "SkipNext", "Download next"));
    if (j.state === "paused") acts.push(b("resume", "Play", "Resume"));
    if (["failed", "cancelled"].includes(j.state)) { acts.push(b("retry", "Retry", "Try again")); if (j.url) acts.push(b("newlink", "Link", "Use a fresh link")); }
    acts.push(ACTIVE.includes(j.state) ? b("cancel", "Close", "Cancel") : b("remove", "Close", "Remove from the list"));
    const msg = j.state === "failed" && j.message ? `<div class="warn more">${esc(j.message.replace(/^Failed: /, ""))}</div>` : "";
    const more = openJobs.has(j.id) ? `<div class="more"><div>${esc(j.filename || "")}</div>${j.dest ? `<div>Into ${esc(j.dest)}</div>` : ""}${j.url ? `<div>From ${esc(j.url)}</div>` : ""}
        <pre>${esc((j.log || []).join("\n"))}</pre></div>` : "";
    const pic = posterFor(j);
    return `<div class="thumb">${pic ? `<img src="${esc(pic)}" alt="" loading="lazy">` : ""}</div>
      <div class="main"><div class="label one title1">${esc(name)}</div><div class="meta sub one">${sub.map(esc).join(" · ")}</div></div>
      ${right ? `<div class="meta size fig">${esc(right)}</div>` : ""}<div class="acts">${acts.join("")}</div>${msg}${more}
      ${bar !== "" ? `<div class="jobbar"><i style="width:${bar}%"></i></div>` : ""}`;
  }
  async function jobClick(e, row) {
    const id = row.dataset.id, btn = e.target.closest("button[data-act]");
    if (!btn) { openJobs.has(id) ? openJobs.delete(id) : openJobs.add(id); render(); return; }
    const j = S.jobs.find(x => x.id === id);
    let act = btn.dataset.act, body = {};
    if (act === "cancel") {
      const working = j.state === "processing" || j.state === "converting";
      const r = await ask({ title: "Cancel this?", text: working ? "This stops the work on it. Try again picks it up later." : "Keep what's downloaded to resume later, or delete it.", buttons: [
        { label: "Keep going", value: "" },
        ...(working ? [{ label: "Stop", value: "keep", cls: "danger" }] : [{ label: "Cancel and keep the part", value: "keep", cls: "danger" }, { label: "Cancel and delete it", value: "delete", cls: "danger" }])] });
      if (!r.value) return;
      body = { delete: r.value === "delete" };
    }
    if (act === "newlink") {
      const r = await ask({ title: "A fresh link to the same file", text: "It carries on where it stopped. The size has to match.", input: true,
        buttons: [{ label: "Back", value: "" }, { label: "Use this link", value: "ok" }] });
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

  async function bodyClick(e) {
    if (e.target.closest("[data-clear]")) { const r = await api("/api/clear", {}); note(`Cleared ${r.removed || 0}`); return; }
    const k = e.target.closest("[data-sec]");
    if (k) { sec = k.dataset.sec; if (sec === "music") loadMusic(); drawLibrary(); return; }
    const row = e.target.closest(".lrow");
    if (!row || e.target.closest(".more")) return;
    if (tab === "downloads") return jobClick(e, row);
    if (tab === "library") {
      const d = e.target.closest("[data-del]");
      if (d) { e.stopPropagation(); remove(d.dataset); return; }
      if (row.dataset.t) openSheet(row.dataset.t);
    }
    if (tab === "system") {
      const s = e.target.closest("[data-set]");
      if (s) setSetting(s.dataset.set, +s.dataset.v);
    }
  }

  // ---------- checking links ----------
  const urlsIn = text => [...new Set((text.match(/https?:\/\/[^\s<>"'`]+/g) || []).map(u => u.replace(/[.,;)\]>'"]+$/, "")))];
  async function check(grow) {
    const ta = $("#links"), urls = urlsIn(ta.value);
    if (!urls.length) { if (ta.value.trim()) note("There's no http(s) link in that"); return; }
    ta.value = ""; grow(); ta.blur();
    const cards = urls.map(u => {
      const el = document.createElement("section"); el.className = "panel"; el.innerHTML = loadingHTML(u);
      $("#probes").append(el);
      return [u, el];
    });
    allBar();
    let i = 0;
    const worker = async () => { while (i < cards.length) { const [u, el] = cards[i++]; await probeInto(u, el); } };
    await Promise.all([worker(), worker(), worker()]);
  }
  const loadingHTML = u => `<div class="facts" style="margin:0"><div class="loading s"></div><span class="one mut">Reading ${esc(u.length > 90 ? u.slice(0, 90) + "…" : u)}</span></div>`;
  const verdict = p => {
    const t = downloadTime(p.size), tail = t ? ` About ${t} to download.` : "";
    const far = p.heavy ? " Away from home it needs a fast connection, or a Light copy." : "";
    if (!p.media) return `Picture and sound details show after the download.${tail}`;
    if (p.play === "direct") return `Plays everywhere as it is.${far}${tail}`;
    if (p.play === "repack") return `Plays everywhere. Browsers get it repackaged as it plays; the file stays as it is.${far}${tail}`;
    return `Plays on TV and phone. This browser can't decode its picture; a Light copy would play here.${tail}`;
  };

  async function probeInto(url, el) {
    const p = await probeLink(url);
    if (!p.ok) {
      el.innerHTML = `<div class="head"><div class="heading">That link didn't work</div><button class="tool" data-x aria-label="Dismiss">${icon("Close")}</button></div>
        <div class="meta mut file">${esc(url)}</div><div class="warn">${esc(p.error)}</div>
        <div class="btns" style="justify-content:flex-end"><button class="word" data-again>Try again</button></div>`;
      $("[data-x]", el).onclick = () => { el.remove(); allBar(); };
      $("[data-again]", el).onclick = () => { el.innerHTML = loadingHTML(url); probeInto(url, el); };
      allBar();
      return;
    }
    el._probe = p; el._kind = p.plan.kind; el._light = null; el._wantLight = false;
    drawProbe(el);
    allBar();
  }
  function drawProbe(el) {
    const p = el._probe, pl = p.plan, m = p.media;
    const single = el._kind === "movie" || el._kind === "tv" && planOf(el).episode !== "";
    const lightBox = p.heavy && single ? `<div class="mchoice">${optHTML("light", el._wantLight, "Add a Light copy", "A smaller version for slow connections, and for browsers that can't decode this one.")}
        ${el._wantLight ? `<input class="mlink" data-lightlink placeholder="Paste the Light copy's link" value="${esc(el._light?.url || "")}" autocomplete="off" autocapitalize="off" spellcheck="false">
        <div data-lightinfo>${lightInfo(el)}</div>` : ""}</div>` : "";
    const both = el._wantLight && el._light?.probe;
    el.innerHTML = `<div class="head"><div class="heading" data-title></div><button class="tool" data-x aria-label="Dismiss" title="Dismiss">${icon("Close")}</button></div>
      ${m ? mediaFacts(m, p.size) : facts(["Size", bytes(p.size)])}
      <p class="meta mut mline">${verdict(p)}</p>
      ${(p.warnings || []).map(w => `<div class="warn">${esc(w)}</div>`).join("")}
      <div class="mline meta mut"><span data-dest></span> <button class="kind mut medit" data-edit>${el._edit ? "Done" : "Change"}</button></div>
      <div data-fields ${el._edit ? "" : "hidden"}>
        <div class="kinds" data-kind><button type="button" class="kind" data-v="movie">Film</button><button type="button" class="kind" data-v="tv">Show</button><button type="button" class="kind" data-v="music">Music</button></div>
        <div class="fields">
          <label class="c4" data-f="title"><span></span><input name="title" autocomplete="off"></label>
          <label class="c2" data-f="year">Year<input name="year" inputmode="numeric" autocomplete="off"></label>
          <label class="c3" data-f="season">Season<input name="season" inputmode="numeric" autocomplete="off"></label>
          <label class="c3" data-f="episode">Episode (blank for a whole season)<input name="episode" inputmode="numeric" autocomplete="off"></label>
        </div></div>
      ${lightBox}
      <div class="btns" style="justify-content:flex-end"><button class="play" data-go>${both ? `Download both · ${bytes((p.size || 0) + (el._light.probe.size || 0))}` : "Download"}</button></div>`;
    const vals = el._vals || pl;
    for (const k of ["title", "year", "season", "episode"]) {
      const inp = $(`input[name=${k}]`, el);
      inp.value = vals[k] ?? "";
      if (k === "title" && el._kind === "music" && !el._vals) inp.value = "";
      inp.oninput = () => { el._vals = planOf(el); refreshPlan(el); };
    }
    for (const b of $$("[data-kind] button", el)) b.onclick = () => {
      if (b.dataset.v === "music" && el._kind !== "music") $("input[name=title]", el).value = "";
      el._kind = b.dataset.v; el._vals = planOf(el); refreshPlan(el);
    };
    $("[data-x]", el).onclick = () => { el.remove(); allBar(); };
    $("[data-edit]", el).onclick = () => { el._edit = !el._edit; el._vals = planOf(el); drawProbe(el); };
    $("[data-go]", el).onclick = () => queue(el);
    for (const o of $$("[data-opt]", el)) o.onclick = () => {
      el._vals = planOf(el);
      if (o.dataset.opt === "light") { el._wantLight = !el._wantLight; }
      drawProbe(el);
      if (o.dataset.opt === "light" && el._wantLight) $("[data-lightlink]", el)?.focus();
    };
    const ll = $("[data-lightlink]", el);
    if (ll) {
      const go = () => { const u = urlsIn(ll.value)[0]; if (u && u !== el._light?.url) checkLight(el, u); };
      ll.addEventListener("paste", () => setTimeout(go, 0));
      ll.addEventListener("keydown", e => { if (e.key === "Enter") { e.preventDefault(); go(); } });
      ll.addEventListener("change", go);
    }
    refreshPlan(el);
  }
  const optHTML = (key, on, name, detail) => `<button type="button" class="opt mopt" data-opt="${key}" aria-pressed="${on}"><span class="tick">${on ? icon("Check") : ""}</span><span class="name">${esc(name)}</span></button>
    ${detail ? `<div class="meta mut optd">${detail}</div>` : ""}`;
  function lightInfo(el) {
    const l = el._light;
    if (!l) return "";
    if (l.loading) return `<div class="facts mline"><div class="loading s"></div><span class="mut meta">Reading the link</span></div>`;
    if (l.error) return `<div class="warn">${esc(l.error)}</div>`;
    const p = l.probe, m = p.media, film = el._probe.media;
    let line = "Details show after the download. It has to run as long as the film, or it's removed.";
    if (m) line = film?.dur && m.dur && Math.abs(film.dur - m.dur) <= 2 ? "Same length as the film. Downloads right after it." : "Downloads right after the film.";
    return `${m ? mediaFacts(m, p.size) : facts(["Size", bytes(p.size)])}<p class="meta mut mline">${line}</p>`;
  }
  async function checkLight(el, url) {
    el._light = { url, loading: true };
    $("[data-lightinfo]", el).innerHTML = lightInfo(el);
    const p = await probeLink(url);
    const film = el._probe.media, m = p.media;
    let error = !p.ok ? p.error : "";
    if (!error && m && !(/^(H\.264|HEVC)/.test(m.vcodec) && m.res !== "4K" && !m.hdr)) error = "A Light copy has to be H.264 or HEVC, 1080p at most and without HDR, so it plays in browsers.";
    if (!error && m && film?.dur && m.dur && Math.abs(film.dur - m.dur) > 2) error = `That file runs ${dur(m.dur)} and the film ${dur(film.dur)}: it's a different cut, so it can't be its Light copy.`;
    el._light = error ? { url, error } : { url, probe: p };
    el._vals = planOf(el); drawProbe(el);
  }
  function planOf(el) {
    const v = n => $(`input[name=${n}]`, el)?.value.trim() ?? "";
    return { ...el._probe.plan, kind: el._kind, title: v("title"), year: v("year"), season: v("season"), episode: v("episode"), mp4: false };
  }
  function refreshPlan(el) {
    const k = el._kind;
    for (const b of $$("[data-kind] button", el)) b.classList.toggle("on", b.dataset.v === k);
    $("[data-f=year]", el).hidden = k !== "movie";
    $("[data-f=season]", el).hidden = $("[data-f=episode]", el).hidden = k !== "tv";
    $("[data-f=title]", el).className = k === "movie" ? "c4" : "c6";
    $("[data-f=title] span", el).textContent = k === "music" ? "Artist (blank to read it from the tags)" : k === "tv" ? "Show" : "Title";
    $("[data-title]", el).textContent = planTitle(planOf(el), el._probe.filename).replace(" · Light copy", "");
    clearTimeout(el._t);
    el._t = setTimeout(async () => {
      const r = await api("/api/plan", { plan: planOf(el) });
      const d = $("[data-dest]", el); if (d) d.textContent = r.error ? r.error : `Goes to ${r.dest}`;
    }, 150);
  }
  async function queue(el) {
    const go = $("[data-go]", el);
    if (el._wantLight && !el._light?.probe) {
      const r = await ask({ title: "Download without a Light copy?", text: el._light?.error ? "The Light copy's link didn't work out." : "There's no link for the Light copy yet.",
        buttons: [{ label: "Back", value: "" }, { label: "Download the film", value: "go" }] });
      if (!r.value) return false;
    }
    go.disabled = true;
    const body = { probe: el._probe, plan: planOf(el) };
    if (el._wantLight && el._light?.probe) body.light = el._light.probe;
    const r = await api("/api/jobs", body);
    if (r.error) { note(r.error); go.disabled = false; return false; }
    el.remove(); allBar();
    note("Queued: " + (r.dest || planOf(el).title));
    tab = "downloads"; $("#body")._k = ""; render();
    return true;
  }
  function allBar() {
    const bx = $("#probes"); if (!bx) return;
    $(".allbar", bx)?.remove();
    const ready = $$(".panel", bx).filter(el => el._probe);
    if (ready.length < 2) return;
    const bar = document.createElement("div");
    bar.className = "btns allbar"; bar.style.justifyContent = "flex-end";
    bar.innerHTML = `<button class="play">Download all ${ready.length}</button>`;
    $("button", bar).onclick = async () => { for (const el of ready) if (!(await queue(el))) break; };
    bx.append(bar);
  }

  // ---------- library ----------
  let sec = "films", query = "", sort = "size";
  async function loadMusic() {
    const r = await api("/api/library");
    if (!r.error) { LIB = r; if (tab === "library" && sec === "music") drawLibrary(); }
  }
  function titleLine(t) {
    if (t.type === "show") {
      const eps = t.episodes || [], first = eps[0]?.info || {}, inB = eps.filter(e => e.browser).length;
      return [plural(t.seasons, "season"), plural(eps.length, "episode"), quality(first), videoWord(first), box(first),
        !eps.length ? "" : inB === eps.length ? "Plays everywhere" : inB ? `${inB} of ${eps.length} in this browser` : "TV and phone only"].filter(Boolean).join(" · ");
    }
    const m = t.info || {};
    return [quality(m), videoWord(m), box(m), t.light ? "Light copy" : plays(t)].filter(Boolean).join(" · ");
  }
  function drawLibrary() {
    const body = $("#body"); if (!body || tab !== "library") return;
    if (body._k !== "lib") {
      body._k = "lib";
      body.innerHTML = `<div class="libbar"><div class="kinds" style="margin:0">${[["films", "Films"], ["shows", "Shows"], ["music", "Music"]].map(([v, l]) => `<button class="kind" data-sec="${v}">${l}</button>`).join("")}</div>
        <label class="query">${icon("Search")}<input id="libQ" type="search" placeholder="Search" autocomplete="off" spellcheck="false" value="${esc(query)}"></label>
        <select id="libSort" aria-label="Sort"><option value="size">Biggest first</option><option value="added">Newest first</option><option value="name">A–Z</option></select></div>
        <div id="libList"></div>`;
      $("#libSort").value = sort;
    }
    for (const b of $$("[data-sec]", body)) b.classList.toggle("on", b.dataset.sec === sec);
    const list = $("#libList");
    if (sec === "music") return drawMusic(list);
    if (!TITLES) { list.innerHTML = `<div class="empty">Loading</div>`; return; }
    const want = sec === "films" ? "movie" : "show";
    const items = TITLES.filter(t => t.type === want && (!query || t.title.toLowerCase().includes(query)))
      .sort((a, b) => sort === "size" ? b.size - a.size : sort === "name" ? a.title.localeCompare(b.title) : b.added - a.added);
    list.innerHTML = !items.length ? `<div class="empty">${query ? `Nothing here matches “${esc(query)}”.` : "Downloads land here once they're done."}</div>`
      : items.map(t => `<div class="lrow" data-t="${t.id}"><div class="thumb">${t.poster ? `<img src="${img(t.poster, "w185")}" alt="" loading="lazy">` : ""}</div>
        <div class="main"><div class="label one title1">${esc(t.title)}${t.year ? ` <span class="mut mw">${t.year}</span>` : ""}</div>
        <div class="meta sub one">${esc(t.busy ? "A job is working on this" : titleLine(t))}</div></div><div class="meta size fig">${bytes(t.size)}</div></div>`).join("");
  }
  function drawMusic(list) {
    if (!LIB) { list.innerHTML = `<div class="empty">Loading</div>`; return; }
    const out = [];
    for (const n of LIB.Music?.children || []) {
      const albums = (n.children || []).filter(c => c.dir);
      for (const a of albums) out.push({ node: a, artist: n.name, title: a.name });
      if (!albums.length || n.children.some(c => !c.dir && AUDIO.test(c.name))) out.push({ node: n, artist: n.name, title: albums.length ? "Singles" : n.name });
    }
    const hits = out.filter(x => !query || (x.title + " " + x.artist).toLowerCase().includes(query))
      .sort((a, b) => sort === "size" ? b.node.size - a.node.size : sort === "name" ? a.title.localeCompare(b.title) : b.node.added - a.node.added);
    list.innerHTML = !hits.length ? `<div class="empty">No music yet.</div>` : hits.map(x => `<div class="lrow"><div class="thumb">${x.node.cover ? `<img src="/api/cover?p=${encodeURIComponent(x.node.cover)}" alt="" loading="lazy">` : ""}</div>
      <div class="main"><div class="label one title1">${esc(x.title)}</div><div class="meta sub one">${esc(x.artist)} · ${plural(x.node.files, "file")}</div></div>
      <div class="meta size fig">${bytes(x.node.size)}</div><div class="acts"><button class="tool" data-del data-path="${esc(x.node.path)}" data-name="${esc(x.title)}" data-size="${x.node.size}" title="Delete" aria-label="Delete ${esc(x.title)}">${icon("Delete")}</button></div></div>`).join("");
  }

  // one title's sheet: its file, where it plays, and what can be done with it
  let sheetId = "", sheetEp = "";
  function closeSheet() { sheetId = sheetEp = ""; $("#mgrSheet")?.remove(); $("#mgrScrim")?.remove(); removeEventListener("keydown", sheetKey, true); }
  const sheetKey = e => { if (e.key === "Escape" && !$(".ask")) { e.preventDefault(); e.stopPropagation(); sheetEp ? (sheetEp = "", drawSheet()) : closeSheet(); } };
  function openSheet(id) {
    sheetId = id; sheetEp = "";
    if (!$("#mgrSheet")) {
      const scrim = document.createElement("div"); scrim.className = "scrim"; scrim.id = "mgrScrim"; scrim.onclick = closeSheet;
      const s = document.createElement("aside"); s.className = "sheet"; s.id = "mgrSheet"; s.tabIndex = -1;
      document.body.append(scrim, s);
      addEventListener("keydown", sheetKey, true);
      s.addEventListener("click", sheetClick);
      s.focus();
    }
    drawSheet();
  }
  const fileSpecs = (f, label = "File") => {
    const m = f.info || {};
    const subs = (m.subs || []).join(", ") + (m.pgs ? " (pictures)" : "");
    return `<div class="group"><div class="caption">${label}</div>
      ${[["Quality", quality(m)], ["Video", videoWord(m)], ["File", [box(m), bytes(f.size - (f.light?.size || 0))].filter(Boolean).join(" · ")], ["Audio", (m.tracks || []).map(t => t.replace(/ · .*$/, "")).filter((t, i, a) => a.indexOf(t) === i).join(", ") || m.audio || ""],
        ["Subtitles", subs], ["Plays in", playsIn(f)]].filter(r => r[1]).map(([k, v]) => `<div class="spec"><span>${k}</span><span>${esc(v)}</span></div>`).join("")}</div>
      ${f.light ? `<div class="group"><div class="caption">Light copy</div>${[["Quality", quality(f.light.info)], ["Video", videoWord(f.light.info)], ["File", `${box(f.light.info)} · ${bytes(f.light.size)}`]]
        .filter(r => r[1]).map(([k, v]) => `<div class="spec"><span>${k}</span><span>${esc(v)}</span></div>`).join("")}</div>` : ""}`;
  };
  const fileActions = f => {
    if (f.busy) return `<p class="meta mut mline">A job is working on this. Downloads shows how it's going.</p>`;
    return `<div class="sactions">
      ${f.heavy && !f.light ? `<button class="word mwide" data-a="light" data-id="${f.id}">Add a Light copy</button>` : ""}
      ${f.light ? `<button class="word mwide danger" data-a="unlight" data-id="${f.id}">Delete the Light copy<span class="meta mut">Frees ${bytes(f.light.size)}</span></button>` : ""}
      <button class="word mwide danger" data-del data-path="${esc(f.path)}" data-name="${esc(f.name)}" data-size="${f.size}">Delete<span class="meta mut">Frees ${bytes(f.size)}</span></button></div>`;
  };
  function drawSheet() {
    const s = $("#mgrSheet"); if (!s) return;
    const t = TITLES?.find(x => x.id === sheetId);
    if (!t) { closeSheet(); return; }
    const close = `<button class="tool" data-close aria-label="Close" title="Close">${icon("Close")}</button>`;
    if (t.type === "movie") {
      s.innerHTML = `<div class="head"><div style="min-width:0"><h2 class="title">${esc(t.title)}</h2><div class="meta mut" style="margin-top:.4rem">${[t.year, t.added ? "added " + ago(t.added).toLowerCase() : ""].filter(Boolean).join(" · ")}</div></div>${close}</div>
        ${fileSpecs(t)}${fileActions({ ...t, name: t.title })}`;
      return;
    }
    const ep = sheetEp && (t.episodes || []).find(e => e.id === sheetEp);
    if (ep) {
      const name = `S${pad2(ep.season)}E${pad2(ep.episode)}`;
      s.innerHTML = `<div class="head"><div style="min-width:0"><button class="kind mut" data-back>${esc(t.title)}</button><h2 class="title" style="margin-top:.4rem">${name} · ${esc(ep.title)}</h2></div>${close}</div>
        ${fileSpecs(ep)}${fileActions({ ...ep, name: `${t.title} ${name}` })}`;
      return;
    }
    const bySeason = {};
    for (const e of t.episodes || []) (bySeason[e.season] ||= []).push(e);
    s.innerHTML = `<div class="head"><div style="min-width:0"><h2 class="title">${esc(t.title)}</h2><div class="meta mut" style="margin-top:.4rem">${[t.year, plural((t.episodes || []).length, "episode"), bytes(t.size)].filter(Boolean).join(" · ")}</div></div>${close}</div>
      ${Object.entries(bySeason).map(([n, eps]) => `<div class="group"><div class="caption">${n === "0" ? "Specials" : "Season " + n}</div>
        ${eps.map(e => `<button class="frow mrow" data-ep="${e.id}"><span class="name meta">${e.episode} · ${esc(e.title)}<span class="mut"> · ${esc(e.light ? "Light copy" : plays(e))}</span></span><span class="meta size fig">${bytes(e.size)}</span></button>`).join("")}</div>`).join("")}
      <div class="sactions"><button class="word mwide danger" data-del data-path="${esc(t.path)}" data-name="${esc(t.title)}" data-size="${t.size}">Delete the show<span class="meta mut">Frees ${bytes(t.size)}</span></button></div>`;
  }
  async function sheetClick(e) {
    if (e.target.closest("[data-close]")) { closeSheet(); return; }
    if (e.target.closest("[data-back]")) { sheetEp = ""; drawSheet(); return; }
    const epb = e.target.closest("[data-ep]");
    if (epb) { sheetEp = epb.dataset.ep; drawSheet(); return; }
    const d = e.target.closest("[data-del]");
    if (d) { if (await remove(d.dataset)) { sheetEp ? (sheetEp = "", drawSheet()) : closeSheet(); } return; }
    const a = e.target.closest("[data-a]"); if (!a) return;
    const id = a.dataset.id;
    if (a.dataset.a === "unlight") {
      const f = findFile(id);
      const r = await ask({ title: "Delete the Light copy?", text: `This frees ${bytes(f?.light?.size)}. Slow connections lose it, and so do browsers that can't decode the film itself.`, buttons: [{ label: "Keep it", value: "" }, { label: "Delete", value: "go", cls: "danger" }] });
      if (!r.value) return;
      const res = await api("/api/manager/light/delete", { id });
      if (res.error) { note(res.error); return; }
      note(`Deleted, ${bytes(res.freed)} freed`); loadTitles();
    }
    if (a.dataset.a === "light") addLight(id);
  }
  const findFile = id => { for (const t of TITLES || []) { if (t.id === id) return t; const e = (t.episodes || []).find(x => x.id === id); if (e) return e; } return null; };
  async function addLight(id) {
    const f = findFile(id);
    const r = await ask({ title: "Add a Light copy", text: "Paste a link to a 1080p H.264 or HEVC file of the same cut. It's checked before it downloads.", input: true,
      buttons: [{ label: "Back", value: "" }, { label: "Check", value: "go" }] });
    if (!r.value || !r.input) return;
    note("Reading the link…");
    const p = await probeLink(r.input);
    if (!p.ok) { note(p.error); return; }
    const m = p.media, film = f?.info;
    if (m && !(/^(H\.264|HEVC)/.test(m.vcodec) && m.res !== "4K" && !m.hdr)) { note("That file is 4K or HDR, so it can't be a Light copy."); return; }
    const same = !m || !film?.dur || Math.abs(film.dur - m.dur) <= 2;
    const ok = await ask({ title: same ? "Download this Light copy?" : "Not the same cut", html: m ? mediaFacts(m, p.size) : "",
      text: same ? (m ? "Same length as the film." : "Its length is checked after the download.") : `That file runs ${dur(m.dur)} and the film ${dur(film.dur)}.`,
      buttons: same ? [{ label: "Back", value: "" }, { label: `Download · ${bytes(p.size)}`, value: "go" }] : [{ label: "Back", value: "" }] });
    if (ok.value !== "go") return;
    const res = await api("/api/manager/light", { id, probe: p });
    if (res.error) { note(res.error); return; }
    note("Queued the Light copy"); loadTitles();
  }
  async function remove({ path, name, size }) {
    const r = await ask({ title: `Delete “${name}”?`, text: `This permanently removes ${bytes(+size)} from mint's disk. There's no undo.`,
      buttons: [{ label: "Keep it", value: "" }, { label: `Delete ${bytes(+size)}`, value: "del", cls: "danger" }] });
    if (r.value !== "del") return false;
    const res = await api("/api/library/delete", { path });
    if (res.error) { note(res.error); return false; }
    note(`Deleted ${name}, ${bytes(res.freed)} freed`);
    loadTitles(); if (sec === "music") loadMusic();
    return true;
  }

  // ---------- system ----------
  function drawSystem() {
    const body = $("#body"); if (!body) return;
    if (!SYS || !S) { if (body._k !== "sys0") { body._k = "sys0"; body.innerHTML = `<div class="empty">Loading</div>`; } return; }
    const L = SYS.library, D = SYS.data, used = L.total - L.free;
    const pct = n => L.total ? Math.max(n > 0 ? .3 : 0, n / L.total * 100).toFixed(2) : 0;
    const parts = [["Films", L.films, "var(--ink)"], ["Shows", L.shows, "var(--muted)"], ["Music", L.music, "var(--overlay)"]];
    const other = Math.max(0, used - L.films - L.shows - L.music);
    const s = S.settings, seg = (key, vals) => `<div class="seg">${vals.map(([v, l]) => `<button data-set="${key}" data-v="${v}" class="${s[key] === v ? "on" : ""}">${l}</button>`).join("")}</div>`;
    const temp = SYS.temp_c ? `${Math.round(SYS.temp_c)}°C` : "";
    const relayed = (SYS.now || []).filter(n => n.relayed).map(n => n.device);
    const html = `<div class="msys"><section><div class="title fig">${bytes(L.free)} free</div><div class="meta mut" style="margin-top:.4rem">of ${bytes(L.total)} on the library disk</div>
        <div class="mbar">${parts.map(([, n, c]) => `<i style="width:${pct(n)}%;background:${c}"></i>`).join("")}</div>
        ${parts.map(([k, n, c]) => `<div class="spec mkey"><span><i class="mdot" style="background:${c}"></i>${k}</span><span class="fig">${bytes(n)}</span></div>`).join("")}
        ${L.light ? `<div class="spec mkey"><span><i class="mdot" style="background:transparent"></i>Light copies, within those</span><span class="fig">${bytes(L.light)}</span></div>` : ""}
        ${other > 5e9 ? `<div class="meta mut mline">Another ${bytes(other)} on that disk isn't the library.</div>` : ""}
        ${D.web ? `<div class="meta mut mline">Browser copies use ${bytes(D.web)} on the system disk, which has ${bytes(D.free)} free.</div>` : ""}</section>
      <section><div class="caption mut">mint</div>
        ${[["Engine", "Running"], ["Downloader", SYS.aria2 ? "Running" : "Not answering"],
          ["Playing now", (SYS.now || []).length ? SYS.now.map(n => `${n.title} on ${n.device}`).join(", ") : "Nothing"],
          ["Processor", SYS.cores ? `${Math.min(100, Math.round(SYS.load / SYS.cores * 100))}% busy` : ""],
          ["Temperature", temp], ["Memory free", `${bytes(SYS.mem_avail)} of ${bytes(SYS.mem_total)}`], ["Last backup", SYS.backup_at ? ago(SYS.backup_at) : "None yet"],
          ["Up for", dur(SYS.uptime_s).replace(/ \d+ min$/, "") || ""]].filter(r => r[1]).map(([k, v]) => `<div class="spec"><span>${k}</span><span class="fig">${esc(v)}</span></div>`).join("")}
        ${(SYS.devices || []).length ? `<div class="caption mut" style="margin-top:2.4rem">Your devices, away from home</div>
          ${SYS.devices.map(d => `<div class="spec"><span>${esc(d.name)}</span><span class="${d.active && !d.direct ? "warnc" : ""}">${!d.online ? "Offline" : !d.active ? "Idle" : d.direct ? "Connected directly" : "Through the relay: too slow for films"}</span></div>`).join("")}` : ""}
        ${relayed.length ? `<div class="warn">${esc(relayed.join(", "))} ${relayed.length > 1 ? "are" : "is"} playing through Tailscale's relay, which carries about 10 Mb/s. The direct path to mint has dropped: the router's port forward for 41641 is the usual cause.</div>` : ""}</section></div>
      <div class="group" style="margin-top:3.2rem"><div class="setrow" style="border-top:0"><div>Download limit<small>MB/s</small></div>${seg("limit_mbps", [[0, "Off"], [5, "5"], [10, "10"], [20, "20"]])}</div>
        <div class="setrow"><div>Downloads at once</div>${seg("max_concurrent", [[1, "1"], [2, "2"], [3, "3"]])}</div>
        <div class="setrow"><div>Media Core for TV and phone<small>Installs the app. It updates itself after that.</small></div>
          <div class="btns" style="margin:0"><a class="word" href="/api/v1/app.apk" download="media-core.apk">TV app</a><a class="word" href="/api/v1/phone-app.apk" download="media-core-phone.apk">Phone app</a></div></div></div>`;
    if (body._k === "sys" && body._h === html) return;
    body._k = "sys"; body._h = html; body.innerHTML = html;
  }
  async function setSetting(key, val) {
    const r = await api("/api/settings", { [key]: val });
    if (r.error) note(r.error); else { S.settings = r; render(); note("Saved"); }
  }

  return { connect, page, busy };
})();
window.Manager = Manager;
