import assert from 'node:assert/strict'
import fs from 'node:fs'
import http from 'node:http'
import path from 'node:path'
import os from 'node:os'
import { chromium, firefox } from 'playwright'
const root = path.resolve(process.argv[2] || 'frontend/dist')
const output =
  process.env.YTMDL_OFFLINE_REPORT_DIR ||
  fs.mkdtempSync(path.join(os.tmpdir(), 'ytmdl-offline-'))
fs.mkdirSync(output, { recursive: true, mode: 0o700 })
const browserOnly = process.argv
  .find((v) => v.startsWith('--browser='))
  ?.split('=')[1]
const audio = Buffer.alloc(44 + 22050 * 12 * 2)
audio.write('RIFF')
audio.writeUInt32LE(audio.length - 8, 4)
audio.write('WAVEfmt ', 8)
audio.writeUInt32LE(16, 16)
audio.writeUInt16LE(1, 20)
audio.writeUInt16LE(1, 22)
audio.writeUInt32LE(22050, 24)
audio.writeUInt32LE(44100, 28)
audio.writeUInt16LE(2, 32)
audio.writeUInt16LE(16, 34)
audio.write('data', 36)
audio.writeUInt32LE(audio.length - 44, 40)
for (let i = 0; i < (audio.length - 44) / 2; i++)
  audio.writeInt16LE(
    Math.round(Math.sin((i * 2 * Math.PI * 440) / 22050) * 3000),
    44 + i * 2,
  )
const tracks = [1, 2].map((n) => ({
  id: 'offline-fixture-' + n,
  title: 'Offline Test ' + n,
  artists: ['Qualification Artist'],
  album: 'Qualification Album',
  album_artist: 'Qualification Artist',
  release_id: 'offline-release',
  track_number: n,
  track_total: 2,
  disc_number: 1,
  disc_total: 1,
  duration_ms: 12000,
  year: 2026,
  source_provider: 'fixture',
  source_id: '',
  source_url: '',
  created_at: new Date().toISOString(),
  codec: 'wav',
}))
const user = {
  id: 'offline-user',
  username: 'qualification',
  role: 'user',
  enabled: true,
}
const playlist = {
  id: 'offline-playlist',
  user_id: user.id,
  name: 'Offline Testliste',
  description: 'Isolated browser fixture',
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
  track_count: 2,
  duration_ms: 24000,
  tracks: tracks.map((t, i) => ({
    ...t,
    position: i + 1,
    added_at: new Date().toISOString(),
  })),
}
let mode = 'normal',
  held = null,
  unexpectedMutations = 0
const server = http.createServer((req, res) => {
  const url = new URL(req.url, 'http://localhost'),
    p = url.pathname
  const json = (data) => {
    res.writeHead(200, {
      'Content-Type': 'application/json',
      'Cache-Control': 'no-store',
    })
    res.end(
      JSON.stringify({
        data,
        meta: {
          count: Array.isArray(data) ? data.length : 1,
          total: Array.isArray(data) ? data.length : 1,
        },
      }),
    )
  }
  if (p.startsWith('/api/')) {
    if (req.method !== 'GET' && req.method !== 'HEAD') {
      unexpectedMutations++
      res.writeHead(405)
      res.end()
      return
    }
    if (p.endsWith('/auth/status'))
      return json({ setup_required: false, authenticated: true, user })
    if (p.endsWith('/auth/me')) return json(user)
    if (p === '/api/v1/playlists/' + playlist.id) return json(playlist)
    if (p.endsWith('/stream')) {
      if (p.includes('fixture-2') && mode === 'failed') {
        res.writeHead(503)
        res.end()
        return
      }
      if (p.includes('fixture-2') && mode === 'oversize') {
        res.writeHead(200, {
          'Content-Type': 'audio/wav',
          'Content-Length': String(70 * 1024 * 1024),
        })
        res.end(audio)
        return
      }
      if (p.includes('fixture-2') && mode === 'hold') {
        res.writeHead(200, { 'Content-Type': 'audio/wav' })
        res.write(audio.subarray(0, 1024))
        held = res
        res.on('close', () => {
          held = null
        })
        return
      }
      const match = /bytes=(\d+)-(\d*)/.exec(req.headers.range || '')
      if (match) {
        const start = Number(match[1]),
          end = Math.min(
            match[2] ? Number(match[2]) : audio.length - 1,
            audio.length - 1,
          )
        res.writeHead(206, {
          'Content-Type': 'audio/wav',
          'Accept-Ranges': 'bytes',
          'Content-Range': `bytes ${start}-${end}/${audio.length}`,
          'Content-Length': end - start + 1,
        })
        res.end(audio.subarray(start, end + 1))
        return
      }
      res.writeHead(200, {
        'Content-Type': 'audio/wav',
        'Content-Length': audio.length,
        'Cache-Control': 'no-store',
      })
      res.end(audio)
      return
    }
    if (p.endsWith('/artwork')) {
      res.writeHead(200, { 'Content-Type': 'image/svg+xml' })
      res.end(
        '<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64"><rect width="64" height="64" fill="#b43b65"/></svg>',
      )
      return
    }
    if (p.endsWith('/queue/summary'))
      return json({ active_jobs: 0, queued_jobs: 0, failed_jobs: 0 })
    return json([])
  }
  const requested = path.resolve(root, '.' + decodeURIComponent(p))
  if (requested !== root && !requested.startsWith(root + path.sep)) {
    res.writeHead(404)
    res.end()
    return
  }
  const file =
    fs.existsSync(requested) && fs.statSync(requested).isFile()
      ? requested
      : path.join(root, 'index.html')
  const types = {
    '.js': 'text/javascript',
    '.css': 'text/css',
    '.html': 'text/html',
    '.json': 'application/json',
    '.svg': 'image/svg+xml',
    '.png': 'image/png',
    '.woff2': 'font/woff2',
  }
  res.writeHead(200, {
    'Content-Type': types[path.extname(file)] || 'application/octet-stream',
    'Cache-Control': 'no-cache',
  })
  res.end(fs.readFileSync(file))
})
await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve))
const base = 'http://127.0.0.1:' + server.address().port,
  reports = []
async function eventually(fn) {
  for (let i = 0; i < 200; i++) {
    if (fn()) return
    await new Promise((resolve) => setTimeout(resolve, 25))
  }
  assert(fn())
}
async function snapshot(page) {
  return page.evaluate(
    () =>
      new Promise((resolve, reject) => {
        const req = indexedDB.open('ytmdl.offline.v1', 1)
        req.onerror = reject
        req.onsuccess = () => {
          const db = req.result,
            tx = db.transaction(['playlists', 'audio'], 'readonly'),
            p = tx.objectStore('playlists').getAll(),
            a = tx.objectStore('audio').getAll()
          tx.oncomplete = () => {
            resolve({
              playlists: p.result.map((x) => ({
                key: x.key,
                saved_at: x.saved_at,
                keys: x.tracks.map((t) => t.offline_blob_key),
              })),
              audio: a.result.length,
            })
            db.close()
          }
        }
      }),
  )
}
try {
  for (const [name, type] of [
    ['chromium', chromium],
    ['firefox', firefox],
  ]) {
    if (browserOnly && browserOnly !== name) continue
    mode = 'normal'
    let step = 'launch'
    const browser = await type.launch({
      headless: true,
      ...(name === 'chromium' && process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE
        ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE }
        : {}),
    })
    try {
      const context = await browser.newContext({
        viewport: { width: 1360, height: 900 },
      })
      await context.addInitScript(() => {
        const Native = window.Audio
        window.__offlineAudio = []
        window.Audio = class extends Native {
          constructor(...args) {
            super(...args)
            window.__offlineAudio.push(this)
          }
        }
        window.EventSource = class {
          addEventListener() {}
          close() {}
        }
      })
      const page = await context.newPage(),
        errors = []
      page.on('pageerror', () => errors.push('Uncaught browser error'))
      page.setDefaultTimeout(20000)
      step = 'copy'
      await page.goto(base + '/playlists/' + playlist.id)
      await page.getByText('Offline mitnehmen · Download-Optionen', { exact: true }).click()
      const copy = page.getByRole('button', {
        name: 'Offline-Kopie speichern / erneuern',
        exact: true,
      })
      await copy.waitFor()
      assert(await copy.isDisabled())
      await page
        .getByLabel('Musik und Metadaten in diesem Browserprofil aufbewahren.')
        .check()
      await copy.click()
      await page
        .getByText(/Musik und App-Hülle sind für den Offline-Start vorbereitet/)
        .waitFor()
      await page.waitForFunction(() => !!navigator.serviceWorker.controller)
      const initial = await snapshot(page)
      assert.equal(initial.playlists.length, 1)
      assert.equal(initial.audio, 2)
      step = 'cancel-final-publish'
      await page.evaluate(() => {
        const getKey = IDBObjectStore.prototype.getKey
        let armed = true
        IDBObjectStore.prototype.getKey = function (...args) {
          const request = getKey.apply(this, args)
          if (
            !armed ||
            this.name !== 'audio' ||
            this.transaction.mode !== 'readwrite'
          )
            return request
          armed = false
          IDBObjectStore.prototype.getKey = getKey
          return new Proxy(request, {
            get(target, property) {
              const value = Reflect.get(target, property, target)
              return typeof value === 'function' ? value.bind(target) : value
            },
            set(target, property, value) {
              if (property === 'onsuccess') {
                target.onsuccess = (event) => {
                  const cancel = [...document.querySelectorAll('button')].find(
                    (b) => b.textContent.trim() === 'Abbrechen',
                  )
                  cancel.click()
                  value(event)
                }
                return true
              }
              return Reflect.set(target, property, value, target)
            },
          })
        }
      })
      await copy.click()
      await page.getByText(/Speichern abgebrochen/).waitFor()
      assert.deepEqual(await snapshot(page), initial)
      step = 'failed-refresh'
      mode = 'failed'
      await copy.click()
      await page.getByText(/Eine Audiodatei ist nicht verfügbar/).waitFor()
      assert.deepEqual(await snapshot(page), initial)
      step = 'bounded-refresh'
      mode = 'oversize'
      await copy.click()
      await page
        .getByText(/Eine Datei überschreitet das Offline-Größenlimit/)
        .waitFor()
      assert.deepEqual(await snapshot(page), initial)
      step = 'canceled-refresh'
      mode = 'hold'
      await copy.click()
      await eventually(() => !!held)
      await page.getByRole('button', { name: 'Abbrechen', exact: true }).click()
      await page.getByText(/Speichern abgebrochen/).waitFor()
      assert.deepEqual(await snapshot(page), initial)
      mode = 'normal'
      step = 'offline-playback'
      await page
        .getByRole('link', { name: 'Offline-Musik öffnen', exact: true })
        .click()
      await page.evaluate(() => {
        const get = IDBObjectStore.prototype.get
        IDBObjectStore.prototype.get = function (...args) {
          const request = get.apply(this, args)
          if (this.name !== 'audio' || this.transaction.mode !== 'readonly')
            return request
          return new Proxy(request, {
            get(target, property) {
              const value = Reflect.get(target, property, target)
              return typeof value === 'function' ? value.bind(target) : value
            },
            set(target, property, value) {
              if (property === 'onsuccess') {
                target.onsuccess = (event) =>
                  setTimeout(() => value(event), 600)
                return true
              }
              return Reflect.set(target, property, value, target)
            },
          })
        }
      })
      await page
        .getByRole('button', { name: 'Offline abspielen', exact: true })
        .click()
      await page
        .getByRole('button', { name: 'Offline pausieren', exact: true })
        .click()
      await page.waitForFunction(
        () =>
          window.__offlineAudio.some(
            (a) => a.src.startsWith('blob:') && a.readyState >= 2,
          ) && window.__offlineAudio.every((a) => a.paused),
      )
      await page
        .getByRole('button', { name: 'Offline wiedergeben', exact: true })
        .click()
      await page.waitForFunction(() =>
        window.__offlineAudio.some(
          (a) => a.src.startsWith('blob:') && !a.paused && a.currentTime > 0.1,
        ),
      )
      const firstURL = await page.evaluate(
        () => window.__offlineAudio.find((a) => !a.paused).src,
      )
      await page
        .getByRole('button', { name: '2 Offline Test 2', exact: true })
        .click()
      await page
        .getByRole('button', { name: '1 Offline Test 1', exact: true })
        .click()
      await page.waitForTimeout(700)
      await page.waitForFunction(
        (url) => window.__offlineAudio.some((a) => a.src === url && !a.paused),
        firstURL,
      )
      await page
        .getByRole('heading', { name: 'Offline Test 1', exact: true })
        .waitFor()
      const cached = await page.evaluate(async () => {
        const results = []
        for (const key of await caches.keys()) {
          const cache = await caches.open(key)
          for (const request of await cache.keys())
            results.push(new URL(request.url).pathname)
        }
        return results
      })
      assert(cached.includes('/offline'))
      assert(!cached.some((p) => p.startsWith('/api/')))
      await page
        .getByRole('button', { name: 'Offline pausieren', exact: true })
        .click()
      await context.setOffline(true)
      step = 'offline-reload'
      await page.reload()
      await page
        .getByRole('button', { name: 'Offline abspielen', exact: true })
        .waitFor()
      await page
        .getByRole('button', { name: 'Offline abspielen', exact: true })
        .click()
      await page.waitForFunction(() =>
        window.__offlineAudio.some(
          (a) => a.src.startsWith('blob:') && !a.paused && a.currentTime > 0.1,
        ),
      )
      await page
        .getByRole('button', { name: 'Offline nächster Titel', exact: true })
        .click()
      await page
        .getByRole('heading', { name: 'Offline Test 2', exact: true })
        .waitFor()
      await page.waitForFunction(() =>
        window.__offlineAudio.some(
          (a) => a.src.startsWith('blob:') && !a.paused && a.currentTime > 0.1,
        ),
      )
      await page.screenshot({
        path: path.join(output, 'offline-' + name + '-desktop.png'),
        fullPage: true,
      })
      await page.setViewportSize({ width: 375, height: 812 })
      assert(
        !(await page.evaluate(
          () => document.documentElement.scrollWidth > innerWidth,
        )),
      )
      await page.screenshot({
        path: path.join(output, 'offline-' + name + '-mobile.png'),
        fullPage: true,
      })
      step = 'local-removal'
      await page
        .getByRole('button', { name: 'Lokale Kopie entfernen', exact: true })
        .click()
      const dialog = page.getByRole('dialog')
      await dialog.waitFor()
      await dialog
        .getByRole('button', { name: 'Abbrechen', exact: true })
        .click()
      assert.deepEqual(await snapshot(page), initial)
      await page
        .getByRole('button', { name: 'Lokale Kopie entfernen', exact: true })
        .click()
      await dialog
        .getByRole('button', {
          name: 'Nur lokale Kopie entfernen',
          exact: true,
        })
        .click()
      await page.getByText(/Noch keine Offline-Playlist/).waitFor()
      const removed = await snapshot(page)
      assert.equal(removed.playlists.length, 0)
      assert.equal(removed.audio, 0)
      assert.equal(unexpectedMutations, 0)
      assert.deepEqual(errors, [])
      step = 'http-capabilities'
      const httpContext = await browser.newContext()
      await httpContext.addInitScript(() => {
        Object.defineProperty(window, 'isSecureContext', { value: false })
        Object.defineProperty(crypto, 'randomUUID', { value: undefined })
        const Native = window.Audio
        window.__offlineAudio = []
        window.Audio = class extends Native {
          constructor(...args) {
            super(...args)
            window.__offlineAudio.push(this)
          }
        }
        window.EventSource = class {
          addEventListener() {}
          close() {}
        }
      })
      const httpPage = await httpContext.newPage()
      await httpPage.goto(base + '/playlists/' + playlist.id)
      await httpPage.getByText('Offline mitnehmen · Download-Optionen', { exact: true }).click()
      await httpPage
        .getByLabel('Musik und Metadaten in diesem Browserprofil aufbewahren.')
        .check()
      await httpPage
        .getByRole('button', {
          name: 'Offline-Kopie speichern / erneuern',
          exact: true,
        })
        .click()
      await httpPage
        .getByText(/Über diese HTTP-Adresse bleibt Offline-Wiedergabe/)
        .waitFor()
      assert.equal(
        await httpPage.evaluate(
          async () => (await navigator.serviceWorker.getRegistrations()).length,
        ),
        0,
      )
      await httpPage
        .getByRole('link', { name: 'Offline-Musik öffnen', exact: true })
        .click()
      await httpPage
        .getByRole('button', { name: 'Offline abspielen', exact: true })
        .click()
      await httpPage.waitForFunction(() =>
        window.__offlineAudio.some(
          (a) => a.src.startsWith('blob:') && !a.paused && a.currentTime > 0.1,
        ),
      )
      await httpContext.close()
      reports.push({
        browser: name,
        explicitConsent: true,
        atomicRefresh: true,
        cancelFinalPublish: true,
        cancelPreserves: true,
        sizeBound: true,
        realBlobPlayback: true,
        pauseDuringSourceLoad: true,
        httpCapabilityFallback: true,
        staleSourceIgnored: true,
        offlineReload: true,
        nextTrack: true,
        noCachedAPI: true,
        localOnlyRemoval: true,
        mobileOverflow: false,
        errors: 0,
      })
      console.log(
        'PASS:',
        name,
        'isolated offline copy, refresh/cancel limits, real playback, service-worker reload and local-only removal',
      )
    } catch (e) {
      fs.writeFileSync(
        path.join(output, 'offline-' + name + '-debug.txt'),
        String(e.stack || e),
      )
      console.error('FAIL:', name, 'offline qualification at', step)
      throw new Error('Offline browser qualification failed')
    } finally {
      await browser.close()
    }
  }
  fs.writeFileSync(
    path.join(output, 'offline-browser-report.json'),
    JSON.stringify(reports, null, 2),
  )
} finally {
  server.closeAllConnections()
  await new Promise((resolve) => server.close(resolve))
}
