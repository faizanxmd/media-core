"use strict";
// Image subtitles (PGS, from Blu-rays), which no browser can show: reads a .sup file and draws each picture on a canvas.
// A .sup is a run of segments, each headed "PG", 90 kHz time, type and size. A display set ends at type 0x80.
function parsePGS(buf) {
  const u = new Uint8Array(buf), d = new DataView(buf);
  const palettes = new Map(), objects = new Map(), events = [];
  let o = 0, comp = null;
  while (o + 13 <= u.length && u[o] === 0x50 && u[o + 1] === 0x47) {
    const t = d.getUint32(o + 2) / 90000, type = u[o + 10], size = d.getUint16(o + 11), p = o + 13;
    if (p + size > u.length) break;
    if (type === 0x16) { // what to show
      if (u[p + 7] & 0x80) { palettes.clear(); objects.clear(); } // a new epoch starts clean
      comp = { t, w: d.getUint16(p), h: d.getUint16(p + 2), palette: u[p + 9], items: [] };
      let q = p + 11;
      for (let n = u[p + 10]; n > 0; n--) {
        const flags = u[q + 3];
        comp.items.push({ id: d.getUint16(q), x: d.getUint16(q + 4), y: d.getUint16(q + 6) });
        q += flags & 0x80 ? 16 : 8;
      }
    } else if (type === 0x14) { // colours
      const pal = new Uint32Array(palettes.get(u[p]) || 256);
      for (let q = p + 2; q + 5 <= p + size; q += 5) {
        const y = 1.164 * (u[q + 1] - 16), cr = u[q + 2] - 128, cb = u[q + 3] - 128;
        const c = v => Math.max(0, Math.min(255, Math.round(v)));
        pal[u[q]] = (u[q + 4] << 24 | c(y + 2.112 * cb) << 16 | c(y - .213 * cb - .533 * cr) << 8 | c(y + 1.793 * cr)) >>> 0;
      }
      palettes.set(u[p], pal);
    } else if (type === 0x15) { // a picture, maybe in several parts
      const id = d.getUint16(p), first = u[p + 3] & 0x80;
      if (first) objects.set(id, { w: d.getUint16(p + 7), h: d.getUint16(p + 9), parts: [u.subarray(p + 11, p + size)] });
      else objects.get(id)?.parts.push(u.subarray(p + 4, p + size));
    } else if (type === 0x80 && comp) {
      const pal = palettes.get(comp.palette);
      events.push({ t: comp.t, w: comp.w, h: comp.h, items: comp.items.map(c => ({ x: c.x, y: c.y, obj: objects.get(c.id), pal })).filter(c => c.obj && c.pal) });
      comp = null;
    }
    o = p + size;
  }
  return events;
}

// Run-length data to ImageData.
function pgsImage(obj, pal) {
  if (!obj.w || !obj.h) return null;
  const out = new Uint32Array(obj.w * obj.h);
  let i = 0;
  for (const part of obj.parts) {
    let p = 0;
    while (p < part.length && i < out.length) {
      const b = part[p++];
      if (b) { out[i++] = pal[b]; continue; }
      const f = part[p++];
      if (!f) { // end of a line: keep to the grid even if a line came up short
        const rem = i % obj.w;
        if (rem) i += obj.w - rem;
        continue;
      }
      const long = f & 0x40, len = long ? (f & 0x3F) << 8 | part[p++] : f & 0x3F;
      const c = f & 0x80 ? pal[part[p++]] : pal[0];
      out.fill(c, i, Math.min(out.length, i + len));
      i += len;
    }
  }
  return new ImageData(new Uint8ClampedArray(out.buffer), obj.w, obj.h);
}

class PGSTrack {
  constructor(buf) { this.events = parsePGS(buf); this.at = -1; }
  // Draws whatever belongs on screen at time t. Cheap when nothing changed.
  draw(canvas, t) {
    const e = this.events;
    let lo = 0, hi = e.length - 1, n = -1;
    while (lo <= hi) { const m = (lo + hi) >> 1; if (e[m].t <= t) { n = m; lo = m + 1; } else hi = m - 1; }
    if (n === this.at) return;
    this.at = n;
    const ev = e[n];
    if (!ev) { canvas.getContext("2d").clearRect(0, 0, canvas.width, canvas.height); return; }
    if (canvas.width !== ev.w || canvas.height !== ev.h) { canvas.width = ev.w; canvas.height = ev.h; }
    const ctx = canvas.getContext("2d");
    ctx.clearRect(0, 0, ev.w, ev.h);
    const jobs = ev.items.map(it => {
      const im = pgsImage(it.obj, it.pal);
      return im ? createImageBitmap(im).then(bm => ({ bm, x: it.x, y: it.y })) : null;
    }).filter(Boolean);
    if (!jobs.length) return;
    Promise.all(jobs).then(list => {
      try {
        if (this.at === n) {
          ctx.clearRect(0, 0, ev.w, ev.h);
          for (const { bm, x, y } of list) ctx.drawImage(bm, x, y);
        }
      } finally {
        for (const { bm } of list) bm.close();
      }
    }).catch(() => {});
  }
}
