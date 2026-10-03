/* Only the public app shell is cached here. Music lives in explicit IndexedDB
 * copies; authenticated API replies and credentials are never cached. */
const VERSION = '__YTMDL_SHELL_VERSION__'
const PREFIX = 'ytmdl-offline-shell-'
const CACHE = PREFIX + VERSION
const isStatic = (path) =>
  path.startsWith('/assets/') ||
  [
    '/favicon.svg',
    '/favicon.png',
    '/favicon-32x32.png',
    '/logo-mark.png',
    '/logo-full.png',
  ].includes(path)
self.addEventListener('install', (event) =>
  event.waitUntil(
    (async () => {
      const response = await fetch('/offline-shell.json', { cache: 'no-store' })
      if (!response.ok) throw new Error('Shell manifest unavailable')
      const manifest = await response.json()
      if (
        manifest.version !== VERSION ||
        !Array.isArray(manifest.assets) ||
        manifest.assets.length > 300 ||
        manifest.assets.some(
          (path) =>
            typeof path !== 'string' ||
            (!['/', '/offline'].includes(path) && !isStatic(path)),
        )
      )
        throw new Error('Shell manifest invalid')
      const cache = await caches.open(CACHE)
      await cache.addAll(manifest.assets)
    })(),
  ),
)
// No forced activation/reload: a playing tab keeps its current application.
self.addEventListener('activate', (event) =>
  event.waitUntil(
    (async () => {
      for (const key of await caches.keys())
        if (key.startsWith(PREFIX) && key !== CACHE) await caches.delete(key)
      await self.clients.claim()
    })(),
  ),
)
self.addEventListener('fetch', (event) => {
  const request = event.request,
    url = new URL(request.url)
  if (
    request.method !== 'GET' ||
    url.origin !== self.location.origin ||
    url.pathname.startsWith('/api/')
  )
    return
  if (request.mode === 'navigate')
    event.respondWith(
      (async () => {
        const cache = await caches.open(CACHE)
        if (url.pathname === '/offline') {
          const saved = await cache.match('/offline')
          if (saved) return saved
        }
        const control = new AbortController(),
          timer = setTimeout(() => control.abort(), 4000)
        try {
          const live = await fetch(request, { signal: control.signal })
          if (live.ok) return live
          return (await cache.match('/offline')) || live
        } catch {
          const saved = await cache.match('/offline')
          if (saved) return saved
          throw new Error('No offline shell')
        } finally {
          clearTimeout(timer)
        }
      })(),
    )
  else if (isStatic(url.pathname))
    event.respondWith(
      (async () => {
        const cache = await caches.open(CACHE)
        return (await cache.match(request)) || fetch(request)
      })(),
    )
})
