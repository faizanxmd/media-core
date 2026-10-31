// Keeps the app shell available so the installed app opens instantly (and
// says "offline" instead of a browser error when mint is unreachable).
// API calls always go to the network.
const CACHE = "media-core-v2";
const SHELL = ["/", "/app.js", "/app.css", "/icon.svg", "/manifest.webmanifest", "/fonts/inter.woff2", "/player.js", "/pgs.js", "/add.js", "/vendor/hls.min.js"];

self.addEventListener("install", e => {
  e.waitUntil(caches.open(CACHE).then(c => c.addAll(SHELL)).then(() => self.skipWaiting()));
});
self.addEventListener("activate", e => {
  e.waitUntil(caches.keys().then(ks => Promise.all(ks.filter(k => k !== CACHE).map(k => caches.delete(k))))
    .then(() => self.clients.claim()));
});
self.addEventListener("fetch", e => {
  const u = new URL(e.request.url);
  if (e.request.method !== "GET" || u.origin !== location.origin || u.pathname.startsWith("/api/")) return;
  const key = u.pathname === "/" ? "/" : u.pathname; // ignore ?url= share params
  e.respondWith(fetch(e.request).then(r => {
    if (r.ok) { const cp = r.clone(); caches.open(CACHE).then(c => c.put(key, cp)); }
    return r;
  }).catch(() => caches.match(key)));
});
