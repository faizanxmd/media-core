"use strict";
// The player: the TV's (docs/design-system.md), with a mouse added. hls.js does what Media3 does in the apps.
// Nothing on screen while playing. A tap of Left/Right skips with a small mark, holding brings the picture strip,
// pausing or moving the mouse shows the title, bar and controls.
// wlog: a short line in the server's journal (POST /api/weblog), so what a browser
// did, Safari's refusals and errors included, can be read on the server.
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

// What this browser can play, asked of the browser itself. Sent to the server with
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

// How fast the server reaches this browser, measured a few seconds after the page loads and again when
// it's ten minutes old. A title with a Light copy plays that when the link can't carry the film.
const Link = (() => {
  let mbps = 0, at = 0, busy = false;
  try { const m = JSON.parse(sessionStorage.getItem("link") || "null"); if (m) { mbps = m.mbps; at = m.at; } } catch {}
  async function pull(bytes) {
    const t0 = performance.now();
    const r = await fetch(`/api/v1/speedtest?bytes=${bytes}`, { cache: "no-store" });
    const n = (await r.arrayBuffer()).byteLength, s = (performance.now() - t0) / 1000;
    return [n * 8 / 1e6 / s, s];
  }
  async function measure() {
    if (busy) return;
    busy = true;
    try {
      let [v, s] = await pull(2 << 20);
      if (s < 1) [v] = await pull(12 << 20); // a quick link needs a longer pull to read true
      mbps = Math.round(v * 10) / 10; at = Date.now();
      try { sessionStorage.setItem("link", JSON.stringify({ mbps, at })); } catch {}
      wlog(`link to ${serverName}: ${mbps} Mb/s`);
    } catch {}
    busy = false;
  }
  setTimeout(() => { if (!at || Date.now() - at > 600000) measure(); }, 4000);
  return { mbps: () => mbps, freshen() { if (Date.now() - at > 600000) measure(); } };
})();

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
  // only from a click, and opening a title waits on the server first, so the click is
  // long gone by then. A play() during any click unlocks the element for good
  // (prime), so it can start on its own later.
  const video = document.createElement("video");
  video.setAttribute("playsinline", ""); video.setAttribute("x-webkit-airplay", "allow"); video.preload = "auto";
  function prime() {
    if (v || video.getAttribute("src")) return;
    const p = video.play();
    if (p) p.catch(() => {});
  }
  let queue = [], ready = {}, item = null, manifest = null, logo = "", cover = "";
  let mode = "hidden", menu = null, scrub = null, jumpedTo = null, holdOrigin = 0, holdResume = false, holdFrom = "hidden", holdSeeked = false;
  let holding = false, holdDir = 0, holdStart = 0, holdLast = 0, holdRaf = 0, keyDown = null, keyTimer = 0;
  let dropsEnd = () => {};
  let skipped = 0, skippedT = 0, commitT = 0, hideT = 0, reportT = 0, tickT = 0, noteT = 0, firstFrame = false;
  let offer = null, dismissedNext = "", nextLeft = 10000, lastTick = 0, ended = false, retried = 0;
  let textCues = null, pgsTrack = null, subIndex = -1, speed = 1, opened = 0, errorShown = false, audioIdx = 0;
  let lastGood = 0, recoveries = 0, cleanSince = 0, stallT = 0, clickT = 0, lastSub = -1, seekPicT = 0, airplay = false;
  let quality = "auto"; // auto | orig | light, for this visit

  const $p = s => el.querySelector(s);
  const langKey = it => it.show_id || it.id;

  // ---------- opening ----------
  async function open(id, { fromStart = false } = {}) {
    const mine = ++opened;
    build();
    // one round of questions, not four in a row: over a slow link each one is a wait
    const [d, web, man] = await Promise.all([api(`/api/v1/item/${id}`), webAPI("/api/v1/web"), webAPI(`/api/v1/web/${id}`)]);
    if (mine !== opened) return;
    if (d.error) { showError("Can't play this", d.error, ""); return; }
    const it = d.item;
    ready = web.ready || {};
    queue = [it]; logo = it.meta?.logo || ""; recoveries = 0;
    start(it, fromStart, man.error ? null : man);
    if (it.type === "episode" && it.show_id) { // the other episodes: Next, and the Episodes row
      const s = await api(`/api/v1/item/${it.show_id}`);
      if (!el || !s.item || !item || !queue.some(x => x.id === id)) return;
      const eps = (s.item.seasons || []).flatMap(x => x.episodes || []);
      if (!eps.some(x => x.id === item.id)) return;
      queue = eps.map(x => x.id === item.id ? item : x);
      logo = s.item.meta?.logo || logo;
      if (!menu) drawFull();
    }
  }

  // man: the answer to "how does it play here", when open already asked. at: start here (a recovery), in seconds.
  async function start(it, fromStart, man, at) {
    const mine = ++opened;
    startAc?.abort(); startAc = new AbortController();
    stopMedia();
    item = it; firstFrame = false; ended = false; errorShown = false; offer = null; textCues = null; pgsTrack = null; subIndex = -1; retried = 0; dismissedNext = "";
    drawCover();
    $p(".offer").innerHTML = ""; $p(".subs").textContent = ""; clearPGS();
    setMode("hidden"); drawFull();
    // Original or Light: as chosen, or by the link's speed when the film needs more than it carries
    Link.freshen();
    const need = it.info?.mbps || 0, have = Link.mbps();
    const slow = quality === "auto" && it.light && have > 0 && need > 0 && have < need * 1.3;
    if (quality === "light" || slow) manifest = await webAPI(`/api/v1/web/${it.id}?v=light`);
    else manifest = man || await webAPI(`/api/v1/web/${it.id}`);
    if (slow && manifest.light && mine === opened) pnote(`Slow connection (${Math.round(have)} Mb/s): playing the Light copy`);
    wlog(`open ${it.id} "${it.show ? it.show + " " : ""}${it.title || ""}": ${manifest.state}, ${manifest.src || (manifest.hls ? "stream" : manifest.state === "ready" ? "HLS copy" : "nothing to play")}, sound ${(manifest.audio || []).map(a => `${a.lang || "?"}/${a.codec}${a.plays === false ? "(no)" : ""}`).join(" ")}`);
    if (mine !== opened) return;
    if (manifest.state === "wait") { showError("Getting this ready", manifest.wait || `${serverLabel()} is preparing this title. Try again in a few minutes.`, ""); return; }
    if (manifest.state !== "ready") { showError("Can't play here", "This browser can't decode this title's picture. A Light copy, added in Manager, would play.", ""); return; }
    const resume = at > 0 ? at : !fromStart && !it.watch?.watched && it.watch?.pos > 0 ? it.watch.pos / 1000 : 0;
    lastGood = resume; cleanSince = 0;
    nowPlaying();
    // a stream the server makes while it plays (the file's picture untouched), or an old copy made ahead
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
        recover(e.details || "the stream stopped");
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

  // Something broke mid-film (the link dropped, a stream piece failed): load it again where it
  // was, quietly, three times before saying so. A stretch of clean playing earns the tries back.
  function recover(why) {
    if (!item || !el || errorShown) return;
    const at = Math.max(0, (v.currentTime > 1 ? v.currentTime : lastGood) - 1);
    wlog(`broke at ${Math.round(at)} s: ${why}; try ${recoveries + 1}`);
    if (recoveries++ >= 3) { showError("Can't play this", `It stopped and didn't come back. Check the connection to ${serverName}.`, String(why || "")); return; }
    busy(true);
    const it = item;
    setTimeout(() => { if (item === it && el) start(it, false, null, at); }, 800 * recoveries);
  }
  // jump: go to a time, and until the film is there show that moment's scrub picture instead of the old frame
  function jump(sec) {
    sec = Math.max(0, Math.min(Math.max(0, dur() / 1000 - 1), sec));
    const inBuffer = [...Array(v.buffered.length).keys()].some(i => v.buffered.start(i) <= sec && sec < v.buffered.end(i) - .5);
    v.currentTime = sec;
    if (!inBuffer) seekPic(true, sec * 1000);
  }
  function seekPic(on, ms) {
    const p = el && $p(".seekpic"); if (!p) return;
    clearTimeout(seekPicT);
    if (!on) { p.classList.remove("on"); return; }
    const u = thumbs?.at(ms) || "";
    if (!u) return;
    p.style.backgroundImage = `url(${u})`;
    seekPicT = setTimeout(() => { if (v?.seeking || v?.readyState < 3) p.classList.add("on"); }, 250); // only when the wait is real
  }
  const canPip = () => !!(document.pictureInPictureEnabled || video.webkitSupportsPresentationMode);
  function togglePip() {
    try {
      if (document.pictureInPictureElement) document.exitPictureInPicture();
      else if (v.requestPictureInPicture) v.requestPictureInPicture().catch(() => {});
      else if (v.webkitSetPresentationMode) v.webkitSetPresentationMode(v.webkitPresentationMode === "picture-in-picture" ? "inline" : "picture-in-picture");
    } catch {}
  }
  // the keyboard's media keys, the Mac's Now Playing, a headset's buttons
  function mediaKeys() {
    const ms = navigator.mediaSession; if (!ms) return;
    const set = (k, fn) => { try { ms.setActionHandler(k, fn); } catch {} };
    set("play", () => { if (v?.paused) toggle(); });
    set("pause", () => { if (v && !v.paused) toggle(); });
    set("seekbackward", () => v && tapSkip(-1));
    set("seekforward", () => v && tapSkip(1));
    set("seekto", e => { if (v && e.seekTime != null) jump(e.seekTime); });
    set("nexttrack", () => { const n = nextOf(); if (n) playNext(n); });
  }
  function nowPlaying() {
    if (!navigator.mediaSession || !window.MediaMetadata || !item) return;
    const [title, sub] = titles(item);
    try { navigator.mediaSession.metadata = new MediaMetadata({ title: item.type === "episode" ? item.title : title, artist: item.type === "episode" ? `${title} · S${item.season}, E${item.episode}` : sub,
      artwork: cover ? [{ src: new URL(cover, location.href).href }] : [] }); } catch {}
  }

  function stopMedia() {
    if (item && v && v.currentTime > 0 && !ended) report(false);
    if (hls) { hls.destroy(); hls = null; }
    if (v) { v.removeAttribute("src"); v.load(); }
    thumbs?.free(); thumbs = null; clearTimeout(stallT); seekPic(false);
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
    dropsEnd(); dropsEnd = () => {};
    clearInterval(reportT); clearInterval(tickT); clearTimeout(hideT); clearTimeout(commitT); clearTimeout(skippedT); clearTimeout(keyTimer);
    clearTimeout(stallT); clearTimeout(clickT); clearTimeout(seekPicT);
    if (document.pictureInPictureElement) document.exitPictureInPicture().catch(() => {});
    if (navigator.mediaSession) try { navigator.mediaSession.metadata = null; } catch {}
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
    $p("canvas.pgs").insertAdjacentHTML("beforebegin", `<div class="layer seekpic"></div>`);
    document.body.append(el);
    document.body.style.overflow = "hidden";
    $p(".vslot").replaceWith(video);
    v = video;
    ac = new AbortController();
    const on = { signal: ac.signal };
    v.volume = +(store.get("volume") ?? 1); v.muted = store.get("muted") === "1";
    v.addEventListener("playing", () => {
      firstFrame = true; $p(".cover").style.opacity = 0; $p(".cover").style.pointerEvents = "none"; busy(false); drawIcons(); clearTimeout(stallT);
      if (!thumbs && item?.thumbs > 0) thumbs = new Thumbs(item.id); // after the film has started, so they don't compete with it
    }, on);
    v.addEventListener("pause", () => { drawIcons(); if (!ended && !holding && mode !== "scrub") { report(false); showFull(); } }, on);
    v.addEventListener("play", drawIcons, on);
    v.addEventListener("waiting", () => {
      busy(true);
      clearTimeout(stallT); // stuck for half a minute: load it again from where it was
      stallT = setTimeout(() => { if (v && item && !v.paused && !ended && v.readyState < 3) recover("stuck waiting for data"); }, 30000);
    }, on);
    v.addEventListener("canplay", () => { busy(false); clearTimeout(stallT); }, on);
    v.addEventListener("seeked", () => { pgsTrack && (pgsTrack.at = -1); busy(false); }, on);
    v.addEventListener("timeupdate", () => { if (!v.seeking) seekPic(false); }, on);
    v.addEventListener("webkitplaybacktargetavailabilitychanged", e => { airplay = e.availability === "available"; const b = $p('[data-a="airplay"]'); if (b) b.hidden = !airplay; }, on);
    v.addEventListener("ended", onEnded, on);
    v.addEventListener("error", () => { if (!hls && v.getAttribute("src")) recover(v.error?.message || `error ${v.error?.code}`); }, on);
    v.addEventListener("loadedmetadata", placePGS, on);
    v.audioTracks?.addEventListener?.("addtrack", applyAudio, on);
    let waits = 0, played = false, seen = null;
    // frames the browser dropped: a line when a 10 s stretch loses more than two, so choppy playback shows in the journal
    const drops = () => {
      const q = v?.getVideoPlaybackQuality?.(); if (!q) return;
      const d = q.droppedVideoFrames - (seen?.d ?? 0), t = q.totalVideoFrames - (seen?.t ?? 0);
      if (seen && d > 2) wlog(`dropped ${d} of ${t} frames before ${Math.round(v.currentTime)} s`);
      seen = { d: q.droppedVideoFrames, t: q.totalVideoFrames };
    };
    dropsEnd = () => { const q = v?.getVideoPlaybackQuality?.(); if (q?.totalVideoFrames) wlog(`frames: ${q.totalVideoFrames} shown, ${q.droppedVideoFrames} dropped`); };
    v.addEventListener("loadedmetadata", () => wlog(`loaded ${v.videoWidth}x${v.videoHeight}, ${Math.round(v.duration)} s, ${v.audioTracks ? v.audioTracks.length + " sound tracks" : "no track list"}`), on);
    v.addEventListener("playing", () => { if (!played) played = true, wlog(`playing at ${Math.round(v.currentTime)} s`); }, on);
    v.addEventListener("error", () => wlog(`video error ${v.error?.code}: ${v.error?.message || ""} (${v.currentSrc.split("/").slice(-2).join("/")})`), on);
    v.addEventListener("stalled", () => wlog(`stalled at ${Math.round(v.currentTime)} s`), on);
    v.addEventListener("waiting", () => { if (++waits <= 3) wlog(`waiting at ${Math.round(v.currentTime)} s`); }, on);
    el.addEventListener("click", onClick);
    el.addEventListener("dblclick", e => { if (e.target === v || e.target.classList.contains("seekpic")) { clearTimeout(clickT); toggleFull(); } });
    mediaKeys();
    el.addEventListener("mousemove", onMove);
    addEventListener("keydown", onKeyDown, true); addEventListener("keyup", onKeyUp, true);
    addEventListener("resize", placePGS);
    clearInterval(tickT); tickT = setInterval(tick, 250);
    requestAnimationFrame(subsLoop);
    clearInterval(reportT); reportT = setInterval(() => { if (v && !v.paused && !ended) { report(false); drops(); } }, 10000);
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
        <div class="pline"><span class="label one">${esc(sub)}</span><span class="body mut fig" id="ends" style="flex:none"></span></div>
        <div class="bar" id="bar" tabindex="0" role="slider" aria-label="Position"><i class="all"></i><i class="buf"></i><i class="now"></i></div>
        <div class="crow">
          <button class="ctl" data-a="toggle" id="ppBtn" aria-label="Play or pause" title="Play or pause (Space)">${icon("Pause")}</button>
          <span class="label fig time"><span id="at">0:00</span><span class="mut" style="font-weight:500" id="len"></span></span>
          <span class="sp"></span>
          <div class="ctls">
            <div class="vol"><input id="volume" type="range" min="0" max="1" step="0.02" value="${v.muted ? 0 : v.volume}" aria-label="Volume" style="--v:${(v.muted ? 0 : v.volume) * 100}%"><button class="ctl" data-a="mute" id="muteBtn" aria-label="Mute" title="Mute (M)">${icon(v.muted ? "Mute" : "Volume")}</button></div>
            ${many ? `<button class="ctl" data-a="episodes" aria-label="Episodes" title="Episodes">${icon("Episodes")}</button>` : ""}
            <button class="ctl" data-a="options" aria-label="Audio, subtitles and speed" title="Audio, subtitles and speed">${icon("Subtitles")}</button>
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
    bar.onpointerup = e => { bar.classList.remove("drag"); const t = at(e); scrub = null; jump(t / 1000); pv.classList.remove("on"); poke(); };
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
    was.btn?.classList?.remove("lit");
    $p("#pmenuBox").innerHTML = "";
    $p(".pwords").style.visibility = "";
    if (!silent) { was.btn?.focus?.(); poke(); }
  }
  // One panel for every choice about how it plays: fixed to the screen's right edge, top to bottom, as on the phone.
  // A pick applies at once and the panel stays, so two things can be set in one visit.
  function openPanel(btn, keep) {
    const speeds = SPEEDS.map(x => ({ title: x === 1 ? "Normal" : x + "×", detail: "", selected: speed === x, pick: () => { speed = x; v.playbackRate = x; } }));
    const more = [];
    if (canPip()) more.push({ title: "Picture in picture", detail: "P", selected: false, pick: () => { closeMenu(); togglePip(); } });
    if (airplay) more.push({ title: "AirPlay", detail: "", selected: false, pick: () => { closeMenu(); v.webkitShowPlaybackTargetPicker?.(); } });
    const audio = audioOptions(), subs = subOptions();
    const sections = [["Audio", audio.length > 1 ? audio : []], ["Subtitles", subs.length > 1 ? subs : []], ["Speed", speeds],
      ["Quality", item.light ? qualityOptions() : []], ["Also", more]].filter(x => x[1].length);
    const flat = sections.flatMap(x => x[1]);
    const at = keep ? $p(".plist")?.scrollTop || 0 : 0, was = keep ? $$(".opt", $p("#pmenuBox")).indexOf(document.activeElement) : -1;
    closeMenu(true);
    const box = $p("#pmenuBox");
    btn.classList.add("lit"); // the button whose panel is open stays lit
    let n = 0;
    box.innerHTML = `<div class="ppanel" role="dialog" aria-label="Audio, subtitles and speed"><div class="plist">${sections.map(([t, opts]) => `<div class="psec"><span class="caption">${esc(t)}</span>
      ${opts.map(o => `<button class="opt" data-i="${n++}"><span class="tick">${o.selected ? icon("Check") : ""}</span><span class="name one">${esc(o.title)}</span>${o.detail ? `<span class="detail">${esc(o.detail)}</span>` : ""}</button>`).join("")}</div>`).join("")}</div></div>`;
    box.onclick = e => {
      const b = e.target.closest("[data-i]"); if (!b) return;
      flat[+b.dataset.i].pick();
      if (menu?.kind === "options") openPanel(btn, true); // redraw the ticks, stay where we were
    };
    menu = { kind: "options", btn };
    clearTimeout(hideT);
    const list = $$(".opt", box);
    if (keep) { $p(".plist").scrollTop = at; list[Math.max(0, was)]?.focus({ preventScroll: true }); }
    else (list.find(o => o.querySelector(".tick svg")) || list[0])?.focus({ preventScroll: true });
  }
  function audioOptions() {
    const list = manifest?.audio || [];
    const langs = {};
    list.forEach(a => { const n = a.name || langName(a.lang, a.title); langs[n] = (langs[n] || 0) + 1; });
    return list.map((a, i) => {
      const n = a.name || langName(a.lang, a.title); // the engine names a track once, for every app
      const detail = a.detail || (isCommentary(a.title) ? "Commentary" : langs[n] > 1 ? (a.channels === 1 ? "Mono" : a.channels === 2 ? "Stereo" : a.channels === 6 ? "5.1" : a.channels === 8 ? "7.1" : "") : "");
      if (a.plays === false) return { title: n, detail: "Not in this browser", selected: false, pick: () => pnote("This browser can't play that track") };
      return { title: n, detail, selected: i === audioIdx, pick: () => pickAudio(i, true) };
    });
  }
  function qualityOptions() {
    const words = i => [i?.res, i?.mbps ? Math.round(i.mbps) + " Mb/s" : ""].filter(Boolean).join(" · ");
    const set = q => { if (q === quality) return; quality = q; start(item, false, null, v.currentTime); };
    return [{ title: "Auto", detail: Link.mbps() ? `Link ${Math.round(Link.mbps())} Mb/s` : "", selected: quality === "auto", pick: () => set("auto") },
      { title: "Original", detail: words(item.info), selected: quality === "orig", pick: () => set("orig") },
      { title: "Light", detail: words(item.light?.info), selected: quality === "light", pick: () => set("light") }];
  }
  function subOptions() {
    const list = manifest?.subs || [];
    if (!list.length) return manifest?.subs_wait ? [{ title: "Being read from the file", detail: "A few minutes", selected: false, pick: () => pnote(`${serverLabel()} is reading this file's subtitles. Open the menu again in a few minutes.`) }] : [];
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
    if (i < 0) i = list.findIndex(a => plays(a) && a.main); // the engine's pick: English, then the file's default
    if (i < 0) i = list.findIndex(a => plays(a) && speaks(a.lang) && !isCommentary(a.title));
    if (i < 0) i = list.findIndex(plays);
    pickAudio(i < 0 ? 0 : i, false);
  }
  async function pickSub(i, quiet) {
    subIndex = i; textCues = null; pgsTrack = null; clearPGS(); $p(".subs").textContent = "";
    if (i >= 0) lastSub = i;
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
  // the file's own subtitles turn up a few minutes after it's first opened: ask the server again
  async function refreshSubs(btn) {
    const mine = item?.id, m = await webAPI(`/api/v1/web/${mine}`);
    if (item?.id !== mine || !manifest || m.state !== "ready") return;
    const before = (manifest.subs || []).length;
    manifest.subs = m.subs || []; manifest.subs_wait = m.subs_wait;
    if (menu?.kind === "options" && manifest.subs.length !== before) openPanel(btn, true);
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
      if (e.detail > 0 && act !== "options" && act !== "episodes") a.blur(); // Space keeps meaning pause after a click
      if (act === "toggle") toggle();
      else if (act === "leave") leave();
      else if (act === "mute") { v.muted = !v.muted; store.set("muted", v.muted ? "1" : "0"); drawFull(); showFull(); }
      else if (act === "fullscreen") toggleFull();
      else if (act === "options") {
        if (menu?.kind === "options") closeMenu();
        else { openPanel(a); if (manifest?.subs_wait) refreshSubs(a); }
      }
      else if (act === "episodes") menu?.kind === "episodes" ? closeMenu() : episodesPanel();
      return;
    }
    if (e.target.closest(".offer")) { takeOffer(); return; }
    if (e.target.closest(".menu, .ppanel, .eppanel, .bar, .vol")) return;
    if (e.target.closest(".perr")) return;
    if (menu) { closeMenu(); return; }
    if (e.pointerType === "touch") { mode === "full" ? setMode("hidden") : showFull(); return; }
    if (e.target === v || e.target.closest(".full, .seekpic")) {
      clearTimeout(clickT);
      if (e.detail > 1) return; // the second click of a double click
      clickT = setTimeout(toggle, 220); // a double click means full screen, not pause and play
    }
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
    if (offer.kind === "intro") { jump(item.intro.end); }
    else { report(true); ended = true; playNext(offer.next); }
    offer = null; drawOffer();
  }
  function tick() {
    if (!el || !item) return;
    drawClock();
    const t = v.currentTime, d = v.duration || 0, now = Date.now();
    if (!v.paused && !v.seeking && v.readyState >= 3 && t > 0) {
      lastGood = t;
      if (!cleanSince) cleanSince = now;
      else if (now - cleanSince > 60000) recoveries = 0;
    } else if (!v.paused) cleanSince = 0;
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
    if (first) { jump(to / 1000); jumpedTo = to; }
    clearTimeout(commitT); clearTimeout(hideT);
    commitT = setTimeout(commit, 350);
    drawClock();
  }
  function commit() {
    clearTimeout(commitT);
    if (scrub == null) return;
    const to = scrub, done = jumpedTo; scrub = null; jumpedTo = null;
    if (to !== done) jump(to / 1000); // a single tap already jumped there; the film has played on since, so don't jump back
    drawClock(); poke();
  }
  function startHold(dir) {
    clearTimeout(skippedT); skipped = 0; $p(".skip.l").classList.remove("on"); $p(".skip.r").classList.remove("on");
    clearTimeout(commitT); clearTimeout(hideT);
    holdFrom = mode; holdResume = !v.paused; holdSeeked = scrub != null; jumpedTo = null;
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
    jump(to / 1000);
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
      case "ArrowUp": case "ArrowDown": { // volume, as every player on a computer has it
        eat();
        if (mode === "scrub") return;
        const to = Math.max(0, Math.min(1, Math.round(((v.muted ? 0 : v.volume) + (k === "ArrowUp" ? .05 : -.05)) * 20) / 20));
        v.volume = to; v.muted = to === 0; store.set("volume", to); store.set("muted", v.muted ? "1" : "0");
        const vol = $p("#volume"); if (vol) { vol.value = to; vol.style.setProperty("--v", to * 100 + "%"); }
        if ($p("#muteBtn")) $p("#muteBtn").innerHTML = icon(v.muted ? "Mute" : "Volume");
        pnote(to ? `Volume ${Math.round(to * 100)}%` : "Muted");
        return;
      }
      case "Tab": if (mode === "hidden") showFull(); return; // Tab walks to the controls
      case "j": case "l": eat(); if (!e.repeat && mode !== "scrub") tapSkip(k === "l" ? 1 : -1); return;
      case "0": case "1": case "2": case "3": case "4": case "5": case "6": case "7": case "8": case "9":
        eat(); if (!e.repeat && mode !== "scrub" && dur() > 0) { jump(dur() / 1000 * +k / 10); pnote(`${+k * 10}%`); } return;
      case "c": { // subtitles on and off
        eat();
        const list = manifest?.subs || [];
        if (!list.length) { pnote(manifest?.subs_wait ? "Subtitles are still being read from the file" : "No subtitles"); return; }
        if (subIndex >= 0) { pickSub(-1); pnote("Subtitles off"); }
        else { const i = lastSub >= 0 && lastSub < list.length ? lastSub : Math.max(0, list.findIndex(x => /^en/i.test(x.lang || ""))); pickSub(i); pnote("Subtitles on"); }
        return;
      }
      case "<": case ">": {
        eat();
        const i = Math.max(0, Math.min(SPEEDS.length - 1, SPEEDS.indexOf(speed) + (k === ">" ? 1 : -1)));
        speed = SPEEDS[i]; v.playbackRate = speed;
        const c = $p('[data-a="speed"]'); if (c) c.innerHTML = speed !== 1 ? speed + "×" : icon("Speed");
        pnote(speed === 1 ? "Normal speed" : speed + "×");
        return;
      }
      case "p": eat(); if (canPip()) togglePip(); return;
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
      e.remove(); errorShown = false; recoveries = 0;
      if (item) start(item, false, null, lastGood); else open(location.hash.split("/")[2]);
    };
    e.querySelector("button").focus();
  }

  return { open, close, prime };
})();
