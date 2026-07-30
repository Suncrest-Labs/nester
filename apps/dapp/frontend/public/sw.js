// Nester DApp service worker.
//
// Caching strategy (documented per #790 acceptance criteria):
// - App shell (/, /offline, manifest, logo) is precached on install.
// - Navigations use network-first, falling back to the cached /offline
//   route when the network is unavailable.
// - Static same-origin GET assets use cache-first with a network fallback
//   that repopulates the cache.
// - Anything under /api/ (including /api/v1/* and /api/intelligence/*) is
//   NEVER cached and always goes straight to the network. Those responses
//   can contain per-user, authenticated data, and caching them in a shared
//   service worker cache would leak one user's data to the next person who
//   uses the same browser profile/device.
// - Cross-origin requests are left untouched (not intercepted).

const CACHE_VERSION = "nester-shell-v1";
const OFFLINE_URL = "/offline";
const APP_SHELL = [OFFLINE_URL, "/manifest.webmanifest", "/logo.png"];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches
      .open(CACHE_VERSION)
      .then((cache) => cache.addAll(APP_SHELL))
      .then(() => self.skipWaiting())
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(
          keys.filter((key) => key !== CACHE_VERSION).map((key) => caches.delete(key))
        )
      )
      .then(() => self.clients.claim())
  );
});

self.addEventListener("fetch", (event) => {
  const { request } = event;
  if (request.method !== "GET") {
    return;
  }

  const url = new URL(request.url);

  // Never intercept cross-origin requests (wallets, RPC nodes, CDNs, etc.).
  if (url.origin !== self.location.origin) {
    return;
  }

  // Never cache API traffic — it may be per-user and authenticated.
  if (url.pathname.startsWith("/api/")) {
    return;
  }

  if (request.mode === "navigate") {
    event.respondWith(
      fetch(request).catch(() =>
        caches.match(OFFLINE_URL).then((cached) => cached || Response.error())
      )
    );
    return;
  }

  event.respondWith(
    caches.match(request).then((cached) => {
      if (cached) {
        return cached;
      }
      return fetch(request)
        .then((response) => {
          if (response.ok) {
            const copy = response.clone();
            caches.open(CACHE_VERSION).then((cache) => cache.put(request, copy));
          }
          return response;
        })
        .catch(() => cached);
    })
  );
});
