"use strict";
// The player: the TV's (docs/design-system.md), with a mouse added. hls.js does what Media3 does in the apps.
// Nothing on screen while playing. A tap of Left/Right skips with a small mark, holding brings the picture strip,
// pausing or moving the mouse shows the title, bar and controls.
// wlog: a short line in mint's journal (POST /api/weblog), so what a browser
// did, Safari's refusals and errors included, can be read on mint.
const wlog = (() => {
  let q = [], t = 0;
  const flush = () => {
    t = 0; if (!q.length) return;
    const body = JSON.stringify({ ua: navigator.userAgent, events: q.splice(0, 40) });
    fetch("/api/weblog", { method: "POST", keepalive: true, headers: { "Content-Type": "application/json" }, body }).catch(() => {});
  };
  addEventListener("pagehide", flush);
  return msg => { q.push(String(msg)); if (!t) t = setTimeout(flush, 1500); };
})();
addEventListener("error", e => wlog(`script error: ${e.message} (${(e.filename || "").split("/").pop()}:${e.lineno})`));
addEventListener("unhandledrejection", e => wlog(`unhandled: ${e.reason?.name || ""} ${e.reason?.message || e.reason}`));

// What this browser can play, asked of the browser itself. Sent to mint with
// every "can it play here" question, so Safari gets 4K, HDR, Dolby Vision and
// Dolby audio as they are, and other browsers what they can take (engine/browser.go).
const browserCan = (async () => {
  const v = document.createElement("video");
  const ok = t => { try { return !!window.MediaSource?.isTypeSupported?.(t) || v.canPlayType(t) === "probably"; } catch { return false; } };
  const tests = {
    h264: 'video/mp4; codecs="avc1.640028"', "h264-10": 'video/mp4; codecs="avc1.6e0028"', hevc: 'video/mp4; codecs="hvc1.2.4.L150.90"',
    av1: 'video/mp4; codecs="av01.0.08M.10"', vp9: 'video/mp4; codecs="vp09.02.40.10"', dv: 'video/mp4; codecs="dvh1.05.06"',
    aac: 'audio/mp4; codecs="mp4a.40.2"', mp3: "audio/mpeg", opus: 'audio/mp4; codecs="opus"', flac: 'audio/mp4; codecs="flac"',
    ac3: 'audio/mp4; codecs="ac-3"', eac3: 'audio/mp4; codecs="ec-3"', alac: 'audio/mp4; codecs="alac"',
  };
  const can = Object.keys(tests).filter(k => ok(tests[k]));
  try {
    const r = await navigator.mediaCapabilities?.decodingInfo({ type: "file", video: { contentType: tests.hevc, width: 3840, height: 2160,
      bitrate: 20e6, framerate: 24, hdrMetadataType: "smpteSt2086", colorGamut: "rec2020", transferFunction: "pq" } });
    if (r?.supported && can.includes("hevc")) can.push("hdr");
  } catch {}
  if ("audioTracks" in HTMLMediaElement.prototype) can.push("switch");
  wlog(`page loaded, can play: ${can.join(",") || "nothing reported"}`);
  return can.join(",");
})();
// a /api/v1/web URL with what this browser can play
async function webAPI(path) { return api(`${path}${path.includes("?") ? "&" : "?"}can=${await browserCan}`); }

const Player = (() => {
  const SPEEDS = [.75, 1, 1.25, 1.5, 2];
  const LANG = (() => { try { return new Intl.DisplayNames(["en"], { type: "language" }); } catch { return null; } })();
  const langName = (code, title) => {
    const c = code && code !== "und" ? code : "";
    let n = "";
    try { if (c) n = LANG?.of(c) || ""; } catch {}
    return n && n !== c ? n : (title || "").split("|")[0].trim() || "Unknown";
  };
  const isCommentary = t => /commentary/i.test(t || "");

  // the scrub pictures: one file per video, read locally so scrubbing never waits on the network
  class Thumbs {
    constructor(id) { this.ready = false; this.urls = new Map(); this.load(id); }
    async load(id) {
      try {
        const r = await fetch(`/api/v1/thumbs/${id}`);
        if (!r.ok) return;
        const b = await r.arrayBuffer(), d = new DataView(b);
        if (String.fromCharCode(...new Uint8Array(b, 0, 4)) !== "MCT1") return;
        this.b = b; this.d = d; this.every = d.getUint32(4); this.n = d.getUint32(8);
        this.ready = this.n > 0;
      } catch {}
    }
    at(ms) {
      if (!this.ready) return "";
      const i = Math.floor(ms / 1000 / this.every);
      if (i < 0 || i >= this.n) return "";
      let u = this.urls.get(i);
      if (!u) {
        const off = k => Number(this.d.getBigUint64(12 + 8 * k));
        u = URL.createObjectURL(new Blob([new Uint8Array(this.b, off(i), off(i + 1) - off(i))], { type: "image/jpeg" }));
        this.urls.set(i, u);
        if (this.urls.size > 400) { const [k, v] = this.urls.entries().next().value; URL.revokeObjectURL(v); this.urls.delete(k); }
      }
      return u;
    }
    free() { for (const u of this.urls.values()) URL.revokeObjectURL(u); this.urls.clear(); this.b = null; }
  }

  // WebVTT, read by hand: the text is drawn by us, so it can move up out of the way of the bar
  function parseVTT(text) {
    const cues = [], time = s => { const p = s.trim().split(":").map(Number); return p.reduce((a, b) => a * 60 + b, 0); };
    for (const block of text.replace(/\r/g, "").split(/\n\n+/)) {
      const lines = block.split("\n"), i = lines.findIndex(l => l.includes("-->"));
      if (i < 0) continue;
      const [a, b] = lines[i].split("-->");
      const words = lines.slice(i + 1).join("\n").replace(/<[^>]+>/g, "").replace(/&amp;/g, "&").replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&nbsp;/g, " ").trim();
      if (words) cues.push({ start: time(a), end: time(b.trim().split(/\s+/)[0]), words });
    }
    return cues;
  }

  let el = null, v = null, hls = null, thumbs = null;
  let ac = null, startAc = null; // listeners of this opening, and of this title's start

  // One video element for the whole visit. Safari lets a video start with sound
  // only from a click, and opening a title waits on mint first, so the click is
  // long gone by then. A play() during any click unlocks the element for good
  // (prime), so it can start on its own later.
  const video = document.createElement("video");
  video.setAttribute("playsinline", ""); video.preload = "auto"; video.disablePictureInPicture = true;
  function prime() {
    if (v || video.getAttribute("src")) return;
    const p = video.play();
    if (p) p.catch(() => {});
  }
  let queue = [], ready = {}, item = null, manifest = null, logo = "", cover = "";
  let mode = "hidden", menu = null, scrub = null, holdOrigin = 0, holdResume = false, holdFrom = "hidden", holdSeeked = false;
  let holding = false, holdDir = 0, holdStart = 0, holdLast = 0, holdRaf = 0, keyDown = null, keyTimer = 0;
  let skipped = 0, skippedT = 0, commitT = 0, hideT = 0, reportT = 0, tickT = 0, noteT = 0, firstFrame = false;
  let offer = null, dismissedNext = "", nextLeft = 10000, lastTick = 0, ended = false, retried = 0;
  let textCues = null, pgsTrack = null, subIndex = -1, speed = 1, opened = 0, errorShown = false, audioIdx = 0;

  const $p = s => el.querySelector(s);
  const langKey = it => it.show_id || it.id;

  // ---------- opening ----------
  async function open(id, { fromStart = false } = {}) {
    const mine = ++opened;
    build();
    const d = await api(`/api/v1/item/${id}`);
    if (mine !== opened) return;
    if (d.error) { showError("Can't play this", d.error, ""); return; }
    const it = d.item;
    const web = await webAPI("/api/v1/web");
    ready = web.ready || {};
    queue = [it]; logo = it.meta?.logo || "";
    if (it.type === "episode" && it.show_id) {
      const s = await api(`/api/v1/item/${it.show_id}`);
      if (s.item) {
        queue = (s.item.seasons || []).flatMap(x => x.episodes || []);
        logo = s.item.meta?.logo || logo;
        if (!queue.some(x => x.id === id)) queue = [it];
        else queue = queue.map(x => x.id === id ? it : x);
      }
    }
    if (mine !== opened) return;
    start(it, fromStart);
  }

  async function start(it, fromStart) {
    const mine = ++opened;
    startAc?.abort(); startAc = new AbortController();
    stopMedia();
    item = it; firstFrame = false; ended = false; errorShown = false; offer = null; textCues = null; pgsTrack = null; subIndex = -1; retried = 0; dismissedNext = "";
    drawCover();
    $p(".offer").innerHTML = ""; $p(".subs").textContent = ""; clearPGS();
    setMode("hidden"); drawFull();
    manifest = await webAPI(`/api/v1/web/${it.id}`);
    wlog(`open ${it.id} "${it.show ? it.show + " " : ""}${it.title || ""}": ${manifest.state}, ${manifest.src || (manifest.hls ? "stream" : manifest.state === "ready" ? "HLS copy" : "nothing to play")}, sound ${(manifest.audio || []).map(a => `${a.lang || "?"}/${a.codec}${a.plays === false ? "(no)" : ""}`).join(" ")}`);
    if (mine !== opened) return;
    if (manifest.state === "wait") { showError("Getting this ready", manifest.wait || "mint is preparing this title. Try again in a few minutes.", ""); return; }
    if (manifest.state !== "ready") { showError("Can't play here", "This browser can't decode this title's picture. A Light copy, added in Manager, would play.", ""); return; }
    thumbs = it.thumbs > 0 ? new Thumbs(it.id) : null;
    const resume = !fromStart && !it.watch?.watched && it.watch?.pos > 0 ? it.watch.pos / 1000 : 0;
    // a stream mint makes while it plays (the file's picture untouched), or an old copy made ahead
    const src = manifest.hls || `/api/v1/web/${it.id}/master.m3u8`;
    if (manifest.src) { // the library file or its Light copy, played as it is
      v.src = manifest.src;
      v.addEventListener("loadedmetadata", () => { if (resume > 0) v.currentTime = resume; pickAudioPref(); go(); }, { once: true, signal: startAc.signal });
    } else if (v.canPlayType("application/vnd.apple.mpegurl") && "audioTracks" in HTMLMediaElement.prototype) {
      // Safari: Apple's own HLS player, which hls.js only imitates (with it, Safari played sound but no picture)
      v.src = src;
      v.addEventListener("loadedmetadata", () => { if (resume > 0) v.currentTime = resume; pickAudioPref(); go(); }, { once: true, signal: startAc.signal });
    } else if (window.Hls && Hls.isSupported()) {
      hls = new Hls({ startPosition: resume > 0 ? resume : -1, maxBufferLength: 60, maxMaxBufferLength: 180, backBufferLength: 60, renderTextTracksNatively: false, subtitleDisplay: false });
      hls.on(Hls.Events.ERROR, (_, e) => {
        if (!e.fatal) return;
        if (e.type === Hls.ErrorTypes.NETWORK_ERROR && retried++ < 5) { setTimeout(() => hls?.startLoad(), 1500 * retried); return; }
        if (e.type === Hls.ErrorTypes.MEDIA_ERROR && retried++ < 3) { hls.recoverMediaError(); return; }
        showError("Can't play this", "The stream stopped and didn't come back.", e.details || "");
      });
      hls.on(Hls.Events.FRAG_LOADED, () => { retried = 0; });
      hls.on(Hls.Events.AUDIO_TRACKS_UPDATED, pickAudioPref);
      hls.attachMedia(v);
      hls.on(Hls.Events.MEDIA_ATTACHED, () => hls.loadSource(src));
      hls.on(Hls.Events.MANIFEST_PARSED, () => go());
    } else if (v.canPlayType("application/vnd.apple.mpegurl")) {
      v.src = src;
      v.addEventListener("loadedmetadata", () => { if (resume > 0) v.currentTime = resume; pickAudioPref(); go(); }, { once: true, signal: startAc.signal });
    } else {
      showError("Can't play this", "This browser can't play the stream.", "");
      return;
    }
    const pref = store.get("t:" + langKey(it));
    const subs = manifest.subs || [];
    const want = pref && pref !== "off" ? subs.findIndex(s => s.lang === pref) : pref === "off" ? -1 : subs.findIndex(s => s.forced);
    if (want >= 0) pickSub(want, true);
    drawFull();
  }
  function go() {
    v.playbackRate = speed;
    const p = v.play();
    if (p) p.catch(e => { wlog(`play() refused: ${e.name}: ${e.message}`); showFull(); }); // a browser that wants a click first
  }

  function stopMedia() {
    if (item && v && v.currentTime > 0 && !ended) report(false);
    if (hls) { hls.destroy(); hls = null; }
    if (v) { v.removeAttribute("src"); v.load(); }
    thumbs?.free(); thumbs = null;
  }

  function report(end) {
    if (!item || !v) return Promise.resolve();
    const dur = Math.round((v.duration || 0) * 1000);
    if (!dur && !end) return Promise.resolve();
    return fetch("/api/v1/progress", { method: "POST", keepalive: true, headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id: item.id, pos: Math.round(v.currentTime * 1000), dur, ended: !!end }) }).catch(() => {});
  }

  function close() {
    opened++;
    if (!el) return;
    if (item && v && v.currentTime > 0 && !ended) report(false);
    clearInterval(reportT); clearInterval(tickT); clearTimeout(hideT); clearTimeout(commitT); clearTimeout(skippedT); clearTimeout(keyTimer);
    cancelAnimationFrame(holdRaf);
    item = null;
    if (hls) { hls.destroy(); hls = null; }
    thumbs?.free(); thumbs = null;
    removeEventListener("keydown", onKeyDown, true); removeEventListener("keyup", onKeyUp, true);
    if (document.fullscreenElement) document.exitFullscreen().catch(() => {});
    ac?.abort(); startAc?.abort(); ac = startAc = null;
    video.removeAttribute("src"); video.load();
    el.remove(); el = null; v = null; menu = null; mode = "hidden"; scrub = null; document.body.style.overflow = "";
  }
  async function leave() {
    if (item && v && v.currentTime > 0 && !ended) await Promise.race([report(false), new Promise(r => setTimeout(r, 1500))]);
    item = null; // already saved
    if (history.length > 1 && !location.hash.endsWith("/direct")) history.back();
    else location.hash = "#/";
  }

  // ---------- the page ----------
  function build() {
    if (el) return;
    el = document.createElement("div");
    el.id = "player"; el.className = "quiet";
    el.innerHTML = `<i class="vslot"></i><canvas class="pgs" hidden></canvas><div class="subs"></div>
      <div class="skip l"></div><div class="skip r"></div><div class="mid" hidden><div class="loading"></div></div>
      <div class="layer scrub"><div class="strip"></div><div class="now2 title fig"></div><div class="bar"><i class="all"></i><i class="buf"></i><i class="now"></i></div></div>
      <div class="layer full"></div><div class="layer cover"></div><div class="offer"></div><div class="pnote" hidden></div>`;
    document.body.append(el);
    document.body.style.overflow = "hidden";
    $p(".vslot").replaceWith(video);
    v = video;
    ac = new AbortController();
    const on = { signal: ac.signal };
    v.volume = +(store.get("volume") ?? 1); v.muted = store.get("muted") === "1";
    v.addEventListener("playing", () => { firstFrame = true; $p(".cover").style.opacity = 0; $p(".cover").style.pointerEvents = "none"; busy(false); drawIcons(); }, on);
    v.addEventListener("pause", () => { drawIcons(); if (!ended && !holding && mode !== "scrub") { report(false); showFull(); } }, on);
    v.addEventListener("play", drawIcons, on);
    v.addEventListener("waiting", () => busy(true), on);
    v.addEventListener("canplay", () => busy(false), on);
    v.addEventListener("seeked", () => { pgsTrack && (pgsTrack.at = -1); busy(false); }, on);
    v.addEventListener("ended", onEnded, on);
    v.addEventListener("error", () => { if (!hls) showError("Can't play this", "The stream stopped and didn't come back.", v.error?.message || ""); }, on);
    v.addEventListener("loadedmetadata", placePGS, on);
    v.audioTracks?.addEventListener?.("addtrack", applyAudio, on);
    let waits = 0, played = false;
    v.addEventListener("loadedmetadata", () => wlog(`loaded ${v.videoWidth}x${v.videoHeight}, ${Math.round(v.duration)} s, ${v.audioTracks ? v.audioTracks.length + " sound tracks" : "no track list"}`), on);
    v.addEventListener("playing", () => { if (!played) played = true, wlog(`playing at ${Math.round(v.currentTime)} s`); }, on);
    v.addEventListener("error", () => wlog(`video error ${v.error?.code}: ${v.error?.message || ""} (${v.currentSrc.split("/").slice(-2).join("/")})`), on);
    v.addEventListener("stalled", () => wlog(`stalled at ${Math.round(v.currentTime)} s`), on);
    v.addEventListener("waiting", () => { if (++waits <= 3) wlog(`waiting at ${Math.round(v.currentTime)} s`); }, on);
    el.addEventListener("click", onClick);
    el.addEventListener("dblclick", e => { if (e.target === v) toggleFull(); });
    el.addEventListener("mousemove", onMove);
    addEventListener("keydown", onKeyDown, true); addEventListener("keyup", onKeyUp, true);
    addEventListener("resize", placePGS);
    clearInterval(tickT); tickT = setInterval(tick, 250);
    requestAnimationFrame(subsLoop);
    clearInterval(reportT); reportT = setInterval(() => { if (v && !v.paused && !ended) report(false); }, 10000);
  }
  function drawCover() {
    const it = item, c = $p(".cover");
    cover = img(it.meta?.backdrop || it.meta?.still, "w1280");
    const [title, sub] = titles(it);
    c.style.opacity = 1; c.style.pointerEvents = "";
    c.innerHTML = `${cover ? `<img src="${cover}" alt="">` : ""}<div class="who"><div class="loading s"></div><div><div class="heading one">${esc(title)}</div>${sub ? `<div class="meta mut one">${esc(sub)}</div>` : ""}</div></div>`;
  }
  function titles(it) { return it.type === "episode" ? [it.show || it.title, epLine(it)] : [displayTitle(it), displayYear(it) ? String(displayYear(it)) : ""]; }

  let busyT = 0;
  function busy(on) {
    clearTimeout(busyT);
    if (!el) return;
    if (!on) { $p(".mid").hidden = true; return; }
    busyT = setTimeout(() => { if (el && firstFrame && mode !== "scrub") $p(".mid").hidden = false; }, 700); // only if the wait is real
  }

  // ---------- title, bar, controls ----------
  function drawFull() {
    if (!item) return;
    const [title, sub] = titles(item);
    const next = nextOf();
    const many = queue.length > 1;
    $p(".full").innerHTML = `<button class="ctl leave" data-a="leave" aria-label="Back" title="Back (Esc)">${icon("Back")}</button>
      <div class="preview"><div class="pic"></div><div class="label fig"></div></div>
      <div class="pwords">
        ${logo ? `<img class="logoimg" src="${img(logo, "w500")}" alt="${esc(title)}">` : `<div class="heading one">${esc(title)}</div>`}
        <div class="pline"><span class="meta one">${esc(sub)}</span><span class="meta mut fig" id="ends" style="flex:none"></span></div>
        <div class="bar" id="bar" tabindex="0" role="slider" aria-label="Position"><i class="all"></i><i class="buf"></i><i class="now"></i></div>
        <div class="crow">
          <button class="ctl" data-a="toggle" id="ppBtn" aria-label="Play or pause">${icon("Pause")}</button>
          <span class="label fig time"><span id="at">0:00</span><span class="mut" style="font-weight:500" id="len"></span></span>
          <span class="sp"></span>
          <div class="ctls">
            <div class="vol"><input id="volume" type="range" min="0" max="1" step="0.02" value="${v.muted ? 0 : v.volume}" aria-label="Volume" style="--v:${(v.muted ? 0 : v.volume) * 100}%"><button class="ctl" data-a="mute" id="muteBtn" aria-label="Mute" title="Mute (M)">${icon(v.muted ? "Mute" : "Volume")}</button></div>
            <button class="ctl" data-a="audio" aria-label="Audio" title="Audio">${icon("Audio")}</button>
            <button class="ctl" data-a="subs" aria-label="Subtitles" title="Subtitles">${icon("Subtitles")}</button>
            <button class="ctl" data-a="speed" aria-label="Speed" title="Speed">${speed !== 1 ? speed + "×" : icon("Speed")}</button>
            ${many ? `<button class="ctl" data-a="episodes" aria-label="Episodes" title="Episodes">${icon("Episodes")}</button>` : ""}
            ${next ? `<button class="ctl" data-a="next" aria-label="Next episode" title="Next episode">${icon("SkipNext")}</button>` : ""}
            <button class="ctl" data-a="fullscreen" aria-label="Full screen" title="Full screen (F)">${icon(document.fullscreenElement ? "Unfull" : "Full")}</button>
          </div></div></div>
      <div id="pmenuBox"></div>`;
    wireBar();
    const vol = $p("#volume");
    vol.oninput = () => { v.muted = +vol.value === 0; v.volume = +vol.value; store.set("volume", v.volume); store.set("muted", v.muted ? "1" : "0"); vol.style.setProperty("--v", vol.value * 100 + "%"); $p("#muteBtn").innerHTML = icon(v.muted ? "Mute" : "Volume"); };
    drawIcons(); drawClock();
  }
  function drawIcons() {
    if (!el) return;
    const b = $p("#ppBtn");
    if (b) b.innerHTML = icon(v.paused ? "Play" : "Pause");
  }
  function dur() { return (v?.duration && isFinite(v.duration) ? v.duration : item?.info?.dur || 0) * 1000; }
  function drawBar(bar) {
    const d = dur() || 1, at = scrub ?? v.currentTime * 1000;
    let buf = 0;
    for (let i = 0; i < v.buffered.length; i++) if (v.buffered.start(i) * 1000 <= at + 1000) buf = Math.max(buf, v.buffered.end(i) * 1000);
    bar.querySelector(".now").style.width = Math.min(100, at / d * 100) + "%";
    bar.querySelector(".buf").style.width = Math.min(100, buf / d * 100) + "%";
  }
  function drawClock() {
    if (!el || !item) return;
    if (mode === "full") {
      const d = dur(), at = scrub ?? v.currentTime * 1000;
      const a = $p("#at"); if (!a) return;
      a.textContent = clock(at);
      $p("#len").textContent = d > 0 ? "  /  " + clock(d) : "";
      const left = (d - at) / (speed || 1);
      $p("#ends").textContent = d > 0 ? "Ends " + new Date(Date.now() + Math.max(0, left)).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" }) : "";
      drawBar($p("#bar"));
    }
    if (mode === "scrub") drawScrub();
  }
  function wireBar() {
    const bar = $p("#bar"), pv = $p(".preview");
    const at = e => { const r = bar.getBoundingClientRect(); return Math.max(0, Math.min(1, (e.clientX - r.left) / r.width)) * dur(); };
    const place = e => {
      const r = el.getBoundingClientRect(), w = pv.offsetWidth, ms = at(e);
      const side = parseFloat(getComputedStyle(document.documentElement).fontSize) * 1.6;
      pv.style.left = Math.max(side, Math.min(r.width - w - side, e.clientX - w / 2)) + "px";
      const u = thumbs?.at(ms) || "";
      const pic = pv.querySelector(".pic"); pic.hidden = !u;
      if (u) pic.style.backgroundImage = `url(${u})`;
      pv.querySelector(".label").textContent = clock(ms);
      pv.classList.add("on");
    };
    bar.onpointermove = e => { place(e); if (bar.classList.contains("drag")) { scrub = at(e); drawClock(); } };
    bar.onpointerleave = () => { if (!bar.classList.contains("drag")) pv.classList.remove("on"); };
    bar.onpointerdown = e => { bar.setPointerCapture(e.pointerId); bar.classList.add("drag"); scrub = at(e); place(e); drawClock(); clearTimeout(hideT); };
    bar.onpointerup = e => { bar.classList.remove("drag"); const t = at(e); scrub = null; v.currentTime = t / 1000; pv.classList.remove("on"); poke(); };
    bar.onpointercancel = () => { bar.classList.remove("drag"); scrub = null; pv.classList.remove("on"); };
  }
  function setMode(m) {
    mode = m;
    if (!el) return;
    $p(".full").classList.toggle("on", m === "full");
    $p(".scrub").classList.toggle("on", m === "scrub");
    el.classList.toggle("chrome", m === "full");
    el.classList.toggle("quiet", m === "hidden");
    $p(".subs").style.visibility = m === "scrub" ? "hidden" : "";
    $p("canvas.pgs").style.visibility = m === "scrub" ? "hidden" : "";
    if (m !== "full") closeMenu(true);
    drawClock(); drawOffer();
  }
  function showFull() { if (!el || mode === "scrub") return; if (mode !== "full") setMode("full"); poke(); }
  function poke() {
    clearTimeout(hideT);
    if (v && !v.paused) hideT = setTimeout(() => { if (mode === "full" && !v.paused && !menu && scrub == null && !$p("#bar:hover") && !$p(".ctls:hover")) setMode("hidden"); }, 5000);
  }
  function onMove(e) {
    if (e.movementX === 0 && e.movementY === 0) return;
    if (mode === "hidden") setMode("full");
    if (mode === "full") poke();
  }

  // ---------- menus ----------
  function closeMenu(silent) {
    if (!menu || !el) { menu = null; return; }
    const was = menu; menu = null;
    $p("#pmenuBox").innerHTML = "";
    $p(".pwords").style.visibility = "";
    if (!silent) { was.btn?.focus?.(); poke(); }
  }
  function openList(kind, btn, title, options, widthRem) {
    closeMenu(true);
    const r = btn.getBoundingClientRect(), rootPx = parseFloat(getComputedStyle(document.documentElement).fontSize);
    const right = Math.max(rootPx * 1.6, innerWidth - r.right);
    const box = $p("#pmenuBox");
    box.innerHTML = `<div class="menu pmenu" style="right:${right}px;width:${widthRem}rem"><span class="caption">${esc(title)}</span>
      ${options.length ? options.map((o, i) => `<button class="opt" data-i="${i}"><span class="tick">${o.selected ? icon("Check") : ""}</span><span class="name one body">${esc(o.title)}</span>${o.detail ? `<span class="detail">${esc(o.detail)}</span>` : ""}</button>`).join("") : `<div class="mut" style="padding:0 0 .8rem 3.6rem">None</div>`}</div>`;
    box.onclick = e => { const b = e.target.closest("[data-i]"); if (b) { options[+b.dataset.i].pick(); closeMenu(); } };
    menu = { kind, btn };
    clearTimeout(hideT);
    const sel = options.findIndex(o => o.selected);
    box.querySelector(`[data-i="${Math.max(0, sel)}"]`)?.focus();
  }
  function audioOptions() {
    const list = manifest?.audio || [];
    const langs = {};
    list.forEach(a => { const n = langName(a.lang, a.title); langs[n] = (langs[n] || 0) + 1; });
    return list.map((a, i) => {
      const n = langName(a.lang, a.title);
      const detail = isCommentary(a.title) ? "Commentary" : langs[n] > 1 ? (a.channels === 1 ? "Mono" : a.channels === 2 ? "Stereo" : a.channels === 6 ? "5.1" : a.channels === 8 ? "7.1" : "") : "";
      if (a.plays === false) return { title: n, detail: "Not in this browser", selected: false, pick: () => pnote("This browser can't play that track") };
      return { title: n, detail, selected: i === audioIdx, pick: () => pickAudio(i, true) };
    });
  }
  function subOptions() {
    const list = manifest?.subs || [];
    if (!list.length) return [];
    return [{ title: "Off", detail: "", selected: subIndex < 0, pick: () => pickSub(-1) },
      ...list.map((s, i) => {
        const t = s.title || "";
        const detail = [s.forced || /forced/i.test(t) ? "Forced" : "", /\b(sdh|cc|hi)\b/i.test(t) ? "SDH" : "", isCommentary(t) ? "Commentary" : ""].filter(Boolean).join(" · ");
        return { title: langName(s.lang, t), detail, selected: i === subIndex, pick: () => pickSub(i) };
      })];
  }
  function pickAudio(i, save) {
    audioIdx = i;
    wlog(`sound track ${i} (${manifest?.audio?.[i]?.lang || "?"}/${manifest?.audio?.[i]?.codec || "?"}), browser lists ${v?.audioTracks ? v.audioTracks.length : "none"}`);
    if (hls) hls.audioTrack = i;
    else applyAudio();
    const a = manifest?.audio?.[i];
    if (save && a?.lang) store.set("a:" + langKey(item), a.lang);
  }
  // Safari fills its track list a moment after the video loads; applied again as each track arrives (build)
  function applyAudio() {
    if (!v?.audioTracks || hls) return;
    for (let k = 0; k < v.audioTracks.length; k++) { const on = k === audioIdx; if (v.audioTracks[k].enabled !== on) v.audioTracks[k].enabled = on; }
  }
  function pickAudioPref() {
    const list = manifest?.audio || [];
    if (!list.length) return;
    const pref = store.get("a:" + langKey(item));
    const mine = (navigator.language || "en").slice(0, 2);
    const speaks = code => { try { return new Intl.Locale(code || "und").language === mine || langName(code).toLowerCase() === (LANG?.of(mine) || "").toLowerCase(); } catch { return false; } };
    const plays = a => a.plays !== false; // only tracks this browser can play
    let i = pref ? list.findIndex(a => plays(a) && a.lang === pref && !isCommentary(a.title)) : -1;
    if (i < 0) i = list.findIndex(a => plays(a) && speaks(a.lang) && !isCommentary(a.title));
    if (i < 0) i = list.findIndex(plays);
    pickAudio(i < 0 ? 0 : i, false);
  }
  async function pickSub(i, quiet) {
    subIndex = i; textCues = null; pgsTrack = null; clearPGS(); $p(".subs").textContent = "";
    const s = manifest?.subs?.[i];
    if (!quiet) store.set("t:" + langKey(item), s ? s.lang : "off");
    if (!s) return;
    const mine = item?.id;
    const r = await fetch(`/api/v1/web/${mine}/${encodeURIComponent(s.file)}`).catch(() => null);
    if (!r?.ok) { pnote("Those subtitles didn't load."); return; }
    if (s.file.endsWith(".sup")) {
      const b = await r.arrayBuffer();
      if (item?.id === mine && subIndex === i) { pgsTrack = new PGSTrack(b); $p("canvas.pgs").hidden = false; placePGS(); }
    } else {
      const t = await r.text();
      if (item?.id === mine && subIndex === i) textCues = parseVTT(t);
    }
  }
  function clearPGS() { const c = $p("canvas.pgs"); if (c) { c.hidden = true; c.getContext("2d").clearRect(0, 0, c.width, c.height); } }
  function placePGS() {
    if (!el || !v?.videoWidth) return;
    const c = $p("canvas.pgs"), W = el.clientWidth, H = el.clientHeight, ar = v.videoWidth / v.videoHeight;
    const w = Math.min(W, H * ar), h = w / ar;
    Object.assign(c.style, { left: (W - w) / 2 + "px", top: (H - h) / 2 + "px", width: w + "px", height: h + "px" });
  }
  let lastText = "";
  function subsLoop() {
    if (!el) return;
    requestAnimationFrame(subsLoop);
    const t = v.currentTime;
    if (pgsTrack) pgsTrack.draw($p("canvas.pgs"), t);
    if (textCues) {
      const words = textCues.filter(c => t >= c.start && t < c.end).map(c => c.words).join("\n");
      if (words !== lastText) { lastText = words; $p(".subs").textContent = words; }
    } else if (lastText) { lastText = ""; $p(".subs").textContent = ""; }
  }
  function episodesPanel() {
    closeMenu(true);
    const box = $p("#pmenuBox");
    $p(".pwords").style.visibility = "hidden";
    box.innerHTML = `<div class="eppanel"><div class="heading">Episodes</div><div class="cards">${queue.map(e => {
      const d = e.info?.dur > 0 ? mins(e.info.dur * 1000) : "";
      return `<div class="e"><button class="card wide" data-e="${e.id}">${e.meta?.still || e.meta?.backdrop ? `<img src="${img(e.meta.still || e.meta.backdrop, "w500")}" alt="" loading="lazy">` : ""}</button>
        <div class="meta one">${e.episode}. ${esc(e.title)}</div><div class="caption one">${e.id === item.id ? "Now playing" : esc(d)}</div></div>`;
    }).join("")}</div></div>`;
    box.onclick = e => {
      const b = e.target.closest("[data-e]"); if (!b) return;
      const ep = queue.find(x => x.id === b.dataset.e);
      if (ep.id === item.id) { closeMenu(); return; }
      if (!ready[ep.id]) { pnote("That episode isn't ready for browsers yet."); return; }
      playNext(ep);
    };
    menu = { kind: "episodes", btn: $p('[data-a="episodes"]') };
    clearTimeout(hideT);
    const cur = box.querySelector(`[data-e="${item.id}"]`); cur?.focus({ preventScroll: true }); cur?.scrollIntoView({ inline: "center", block: "nearest" });
  }

  // ---------- doing things ----------
  function toggle() {
    if (ended) { ended = false; v.currentTime = 0; }
    if (v.paused) { v.play().catch(() => {}); setMode("hidden"); }
    else { v.pause(); showFull(); }
  }
  async function toggleFull() {
    try { if (document.fullscreenElement) await document.exitFullscreen(); else await el.requestFullscreen(); } catch {}
    const b = $p('[data-a="fullscreen"]'); if (b) b.innerHTML = icon(document.fullscreenElement ? "Unfull" : "Full");
  }
  function onClick(e) {
    const a = e.target.closest("[data-a]");
    if (a) {
      const act = a.dataset.a;
      if (e.detail > 0 && act !== "audio" && act !== "subs" && act !== "speed" && act !== "episodes") a.blur(); // Space keeps meaning pause after a click
      if (act === "toggle") toggle();
      else if (act === "leave") leave();
      else if (act === "mute") { v.muted = !v.muted; store.set("muted", v.muted ? "1" : "0"); drawFull(); showFull(); }
      else if (act === "fullscreen") toggleFull();
      else if (act === "next") { const n = nextOf(); if (n) playNext(n); }
      else if (act === "audio") menu?.kind === "audio" ? closeMenu() : openList("audio", a, "Audio", audioOptions(), 28);
      else if (act === "subs") menu?.kind === "subs" ? closeMenu() : openList("subs", a, "Subtitles", subOptions(), 28);
      else if (act === "speed") menu?.kind === "speed" ? closeMenu() : openList("speed", a, "Speed", SPEEDS.map(s => ({ title: s === 1 ? "Normal" : s + "×", detail: "", selected: speed === s, pick: () => { speed = s; v.playbackRate = s; const c = $p('[data-a="speed"]'); if (c) c.innerHTML = s !== 1 ? s + "×" : icon("Speed"); } })), 18);
      else if (act === "episodes") menu?.kind === "episodes" ? closeMenu() : episodesPanel();
      return;
    }
    if (e.target.closest(".offer")) { takeOffer(); return; }
    if (e.target.closest(".menu, .eppanel, .bar, .vol")) return;
    if (e.target.closest(".perr")) return;
    if (menu) { closeMenu(); return; }
    if (e.pointerType === "touch") { mode === "full" ? setMode("hidden") : showFull(); return; }
    if (e.target === v || e.target.closest(".full")) toggle();
  }
  const nextOf = () => { const i = queue.findIndex(x => x.id === item?.id); const n = i >= 0 ? queue[i + 1] : null; return n && ready[n.id] ? n : null; };
  async function playNext(n) {
    closeMenu(true);
    if (location.hash !== `#/play/${n.id}`) history.replaceState(null, "", `#/play/${n.id}`);
    const fresh = (await api(`/api/v1/item/${n.id}`)).item || n;
    queue = queue.map(x => x.id === n.id ? fresh : x);
    start(fresh, false);
  }
  function onEnded() {
    ended = true;
    report(true);
    const n = nextOf();
    if (n) playNext(n);
    else leave();
  }

  // ---------- offers: Skip intro, the next episode ----------
  function drawOffer() {
    if (!el) return;
    const box = $p(".offer");
    if (!offer || mode === "scrub") { box.innerHTML = ""; box._k = ""; return; }
    const lit = mode !== "full";
    const k = offer.kind + (offer.next?.id || "") + lit + (offer.kind === "next" ? offer.count : "");
    if (box._k === k) return;
    box._k = k;
    box.innerHTML = offer.kind === "intro"
      ? `<button class="word ic ${lit ? "lit" : ""}">Skip intro${icon("SkipNext")}</button>`
      : `<button class="nextc ${lit ? "lit" : ""}"><div class="pic" style="${offer.next.meta?.still ? `background-image:url('${img(offer.next.meta.still, "w300")}')` : ""}">${offer.count ? `<span class="count fig">${offer.count}</span>` : ""}</div>
          <div style="min-width:0"><div class="small">Next Episode</div><div class="label two">${esc(offer.next.title)}</div><div class="small">Season ${offer.next.season}, Episode ${offer.next.episode}</div></div></button>`;
  }
  function takeOffer() {
    if (!offer) return;
    if (offer.kind === "intro") { v.currentTime = item.intro.end; }
    else { report(true); ended = true; playNext(offer.next); }
    offer = null; drawOffer();
  }
  function tick() {
    if (!el || !item) return;
    drawClock();
    const t = v.currentTime, d = v.duration || 0, now = Date.now();
    let o = null;
    const creditsAt = item.credits?.start ?? (d > 0 ? d - 40 : Infinity), n = nextOf();
    if (item.intro && t >= item.intro.start && t < item.intro.end - 2) o = { kind: "intro" };
    else if (n && item.type === "episode" && dismissedNext !== item.id && t >= creditsAt && d - t > 3) o = { kind: "next", next: n };
    if (o?.kind === "next" && offer?.kind !== "next") nextLeft = 10000;
    if (o?.kind === "next" && !v.paused && !menu) {
      nextLeft -= Math.min(1000, now - lastTick);
      if (nextLeft <= 0) { offer = o; takeOffer(); lastTick = now; return; }
    }
    if (o?.kind === "next") o.count = Math.max(0, Math.min(10, Math.ceil(nextLeft / 1000)));
    lastTick = now;
    offer = o; drawOffer();
  }

  // ---------- skips and the picture strip ----------
  function tapSkip(dir) {
    const first = scrub == null;
    if (first) holdOrigin = v.currentTime * 1000;
    const from = scrub ?? v.currentTime * 1000;
    const to = Math.max(0, Math.min(Math.max(0, dur() - 1000), from + dir * 10000));
    scrub = to;
    if (mode === "hidden") {
      skipped += Math.round((to - from) / 1000);
      const on = skipped > 0 ? ".skip.r" : ".skip.l", off = skipped > 0 ? ".skip.l" : ".skip.r";
      $p(off).classList.remove("on");
      if (skipped) { $p(on).innerHTML = `${icon(skipped > 0 ? "SkipAhead" : "SkipBack")}<span class="fig">${Math.abs(skipped)}</span>`; $p(on).classList.add("on"); }
      clearTimeout(skippedT);
      skippedT = setTimeout(() => { skipped = 0; $p(".skip.l")?.classList.remove("on"); $p(".skip.r")?.classList.remove("on"); }, 1000);
    }
    if (first) v.currentTime = to / 1000;
    clearTimeout(commitT); clearTimeout(hideT);
    commitT = setTimeout(commit, 350);
    drawClock();
  }
  function commit() {
    clearTimeout(commitT);
    if (scrub == null) return;
    const to = scrub; scrub = null;
    if (Math.abs(v.currentTime * 1000 - to) > 200) v.currentTime = to / 1000;
    drawClock(); poke();
  }
  function startHold(dir) {
    clearTimeout(skippedT); skipped = 0; $p(".skip.l").classList.remove("on"); $p(".skip.r").classList.remove("on");
    clearTimeout(commitT); clearTimeout(hideT);
    holdFrom = mode; holdResume = !v.paused; holdSeeked = scrub != null;
    if (!holdSeeked) holdOrigin = v.currentTime * 1000;
    scrub = scrub ?? v.currentTime * 1000;
    holding = true; // so the pause below doesn't bring the controls up
    v.pause();
    setMode("scrub");
    runHold(dir);
  }
  function runHold(dir) {
    holdDir = dir; holdStart = holdLast = Date.now(); holding = true;
    cancelAnimationFrame(holdRaf);
    const step = () => {
      if (mode !== "scrub" || !holding) return;
      const now = Date.now(), held = now - holdStart;
      const rate = held < 1500 ? 60 : held < 4000 ? 180 : 480; // 1, then 3, then 8 minutes of film per second
      scrub = Math.max(0, Math.min(Math.max(0, dur() - 1000), scrub + holdDir * rate * (now - holdLast)));
      holdLast = now;
      drawScrub();
      holdRaf = requestAnimationFrame(step);
    };
    holdRaf = requestAnimationFrame(step);
  }
  function stopHold() { holding = false; cancelAnimationFrame(holdRaf); }
  function landHold() {
    stopHold();
    const to = scrub ?? 0;
    scrub = null; setMode("hidden");
    v.currentTime = to / 1000;
    v.play().catch(() => {});
  }
  function cancelHold() {
    stopHold();
    scrub = null;
    if (holdSeeked) v.currentTime = holdOrigin / 1000;
    if (holdResume) v.play().catch(() => {});
    setMode(holdFrom === "scrub" ? "hidden" : holdFrom);
    poke();
  }
  function drawScrub() {
    const here = Math.floor((scrub ?? v.currentTime * 1000) / 10000) * 10000, s = 30000, base = Math.floor(here / s) * s, d = dur();
    const strip = $p(".strip");
    if (thumbs?.ready) {
      const key = String(here);
      if (strip._k !== key) {
        strip._k = key;
        const cell = (ms, big) => {
          const ok = ms >= 0 && ms <= d, u = ok ? thumbs.at(ms) : "";
          return `<div class="${big ? "big" : ""} ${ok ? "" : "out"}" style="${u ? `background-image:url(${u})` : ""}"></div>`;
        };
        strip.innerHTML = [base - 2 * s, base - s].map(m => cell(m)).join("") + cell(here, true) + [base + s, base + 2 * s].map(m => cell(m)).join("");
      }
    } else strip.innerHTML = "";
    $p(".now2").textContent = clock(scrub ?? v.currentTime * 1000);
    drawBar($p(".scrub .bar"));
  }

  // ---------- keys ----------
  function onKeyDown(e) {
    if (!el) return;
    if (e.ctrlKey || e.metaKey || e.altKey) return;
    const k = e.key, a = document.activeElement;
    const eat = () => { e.preventDefault(); e.stopPropagation(); };
    if ($p(".perr")) { if (k === "Escape") { eat(); leave(); } return; }
    if (k === "Escape" || k === "Backspace") {
      eat();
      if (menu) closeMenu();
      else if (mode === "scrub") cancelHold();
      else if (offer?.kind === "next") { dismissedNext = item.id; offer = null; drawOffer(); }
      else if (document.fullscreenElement) toggleFull();
      else leave();
      return;
    }
    if (menu) {
      const list = $$(".opt, .card", $p("#pmenuBox"));
      const i = list.indexOf(a);
      const across = menu.kind === "episodes";
      const fwd = across ? "ArrowRight" : "ArrowDown", back = across ? "ArrowLeft" : "ArrowUp";
      if (k === fwd || k === back) { eat(); const t = list[Math.max(0, Math.min(list.length - 1, i + (k === fwd ? 1 : -1)))]; t?.focus({ preventScroll: true }); t?.scrollIntoView({ block: "nearest", inline: "nearest" }); }
      else if (k.startsWith("Arrow")) eat();
      else if (k === "Enter" || k === " ") { if (!a?.matches("button")) eat(); }
      return;
    }
    if (a?.matches(".ctl, .vol input") && !k.startsWith("F")) { // on the controls, arrows walk along them
      const ctls = $$(".crow .ctl", $p(".full")).filter(x => x.offsetParent);
      const i = ctls.indexOf(a);
      if (k === "ArrowLeft" || k === "ArrowRight") { if (i >= 0) { eat(); ctls[Math.max(0, Math.min(ctls.length - 1, i + (k === "ArrowRight" ? 1 : -1)))].focus(); poke(); return; } }
      if (k === "ArrowUp") { eat(); $p("#bar").focus(); poke(); return; }
      if (k === "ArrowDown") { eat(); return; }
      if (k === "Enter" || k === " ") { poke(); return; }
    }
    switch (k) {
      case " ": case "k": case "Enter":
        eat();
        if (e.repeat) return;
        if (mode === "scrub") landHold();
        else if (scrub != null) commit();
        else if (k === "Enter" && offer && mode !== "full") takeOffer();
        else toggle();
        return;
      case "ArrowLeft": case "ArrowRight": {
        eat();
        if (e.repeat) return;
        const dir = k === "ArrowRight" ? 1 : -1;
        keyDown = { dir, at: Date.now() };
        clearTimeout(keyTimer);
        keyTimer = setTimeout(() => {
          if (!keyDown) return;
          if (mode === "scrub") runHold(keyDown.dir); else startHold(keyDown.dir);
        }, 350);
        return;
      }
      case "ArrowUp": case "ArrowDown":
        eat();
        if (e.repeat) return;
        if (mode === "scrub") return;
        if (mode === "hidden") { showFull(); if (k === "ArrowDown") $$(".crow .ctl", $p(".full"))[0]?.focus(); else $p("#bar")?.focus(); }
        else if (k === "ArrowDown") { $$(".crow .ctl", $p(".full")).find(x => x.offsetParent)?.focus(); poke(); }
        else { $p("#bar")?.focus(); poke(); }
        return;
      case "f": eat(); toggleFull(); return;
      case "m": eat(); v.muted = !v.muted; store.set("muted", v.muted ? "1" : "0"); if ($p("#muteBtn")) $p("#muteBtn").innerHTML = icon(v.muted ? "Mute" : "Volume"); pnote(v.muted ? "Muted" : "Sound on"); return;
      case "n": { eat(); const n = nextOf(); if (n) playNext(n); return; }
    }
  }
  function onKeyUp(e) {
    if (!el || (e.key !== "ArrowLeft" && e.key !== "ArrowRight") || !keyDown) return;
    const was = keyDown; keyDown = null;
    clearTimeout(keyTimer);
    if (mode === "scrub" && holding) { stopHold(); return; } // the strip stays up after letting go
    if (Date.now() - was.at < 350) {
      if (mode === "scrub") { scrub = Math.max(0, Math.min(dur() - 1000, scrub + was.dir * 10000)); drawScrub(); }
      else tapSkip(was.dir);
    }
  }
  function pnote(text) {
    const n = $p(".pnote"); n.textContent = text; n.hidden = false;
    clearTimeout(noteT); noteT = setTimeout(() => { if (el) n.hidden = true; }, 2500);
  }
  function showError(title, msg, detail) {
    if (errorShown || !el) return;
    errorShown = true;
    const e = document.createElement("div");
    e.className = "layer perr";
    e.innerHTML = `<div><div class="title">${esc(title)}</div><p class="body mut" style="margin-top:.8rem">${esc(msg)}</p>${detail ? `<p class="caption mut" style="margin-top:.8rem">${esc(detail)}</p>` : ""}
      <div class="btns" style="margin-top:2.4rem"><button class="word" data-x="retry">Try again</button><button class="word" data-x="close">Close</button></div></div>`;
    el.append(e);
    $p(".cover").style.opacity = 0;
    e.onclick = ev => {
      const b = ev.target.closest("[data-x]"); if (!b) return;
      ev.stopPropagation();
      if (b.dataset.x === "close") { leave(); return; }
      e.remove(); errorShown = false;
      if (item) start(item, false); else open(location.hash.split("/")[2]);
    };
    e.querySelector("button").focus();
  }

  return { open, close, prime };
})();
