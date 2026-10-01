import assert from 'node:assert/strict'
import fs from 'node:fs'
import { execFileSync } from 'node:child_process'
import { chromium, firefox } from 'playwright'

const state = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'))
const password = 'qualification-password-123'

async function api(page, path, method = 'GET', body) {
  return page.evaluate(async ({ path, method, body }) => {
    const csrf = document.cookie.split('; ').find(c => c.startsWith('ytmdl_csrf='))?.split('=').slice(1).join('=')
    const response = await fetch('/api/v1' + path, {
      method, headers: { 'Content-Type': 'application/json', ...(csrf ? { 'X-CSRF-Token': decodeURIComponent(csrf) } : {}) },
      ...(body ? { body: JSON.stringify(body) } : {}),
    })
    return { status: response.status, data: await response.json() }
  }, { path, method, body })
}

async function login(page, username) {
  await page.goto(state.base)
  await page.getByLabel('Benutzername', { exact: true }).fill(username)
  await page.getByLabel('Passwort', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Anmelden', exact: true }).click()
  await page.getByRole('link', { name: 'Entdecken', exact: true }).first().waitFor()
}

async function playback(page) {
  await page.goto(state.base + '/library?view=artists')
  await page.getByText('Qualification Artist', { exact: true }).first().click()
  await page.getByText('Qualification Album', { exact: true }).first().click()
  await page.getByRole('button', { name: 'Album abspielen', exact: true }).click()
  await page.waitForFunction(() => window.__qualificationAudio.some(a => a.readyState >= 2 && a.currentTime > 0.1 && !a.paused), { timeout: 15000 })
  await page.getByRole('button', { name: 'Pause', exact: true }).first().click()
  assert(await page.evaluate(() => window.__qualificationAudio.every(a => a.paused)))
}

try {
for (const [name, type] of [['chromium', chromium], ['firefox', firefox]]) {
  if (process.argv.includes('--only-firefox') && name !== 'firefox') continue
  const browser = await type.launch({ headless: true })
  try {
    const context = await browser.newContext({ viewport: { width: 1360, height: 900 } })
    await context.addInitScript(() => {
      const NativeAudio = window.Audio
      window.__qualificationAudio = []
      window.Audio = function (...args) {
        const audio = new NativeAudio(...args)
        window.__qualificationAudio.push(audio)
        return audio
      }
      window.Audio.prototype = NativeAudio.prototype
    })
    const page = await context.newPage()
    page.setDefaultTimeout(15000)
    const errors = []
    page.on('pageerror', () => errors.push('Uncaught browser error'))
    if (name === 'chromium') {
      await page.goto(state.base)
      await page.getByLabel('Benutzername', { exact: true }).fill('qualification-admin')
      await page.getByLabel('Passwort', { exact: true }).fill(password)
      if (await page.getByRole('button', { name: 'Administrator erstellen', exact: true }).isVisible()) {
        await page.getByLabel('Passwort bestätigen', { exact: true }).fill(password)
        await page.getByRole('button', { name: 'Administrator erstellen', exact: true }).click()
      } else {
        await page.getByRole('button', { name: 'Anmelden', exact: true }).click()
      }
      await page.getByRole('link', { name: 'Entdecken', exact: true }).first().click()
      await page.getByRole('searchbox', { name: 'Künstler, Album oder URL suchen' }).fill('Qualification')
      await page.getByRole('button', { name: 'Suchen', exact: true }).click()
      await page.getByText('Qualification Artist', { exact: true }).first().click()
      await page.getByText('Qualification Album', { exact: true }).first().click()
      const started = page.waitForResponse(r => r.url().endsWith('/api/v1/downloads/release') && r.request().method() === 'POST')
      await page.getByRole('button', { name: 'Release herunterladen', exact: true }).click()
      const response = await started
      assert.equal(response.status(), 202)
      const job = (await response.json()).data
      const deadline = Date.now() + 60000
      let done = false
      while (Date.now() < deadline) {
        const current = (await api(page, '/jobs/' + job.id)).data.data.job
        if (current.status === 'failed') throw new Error('Synthetic browser acquisition failed')
        if (current.status === 'completed') { done = true; break }
        await new Promise(resolve => setTimeout(resolve, 500))
      }
      assert(done, 'Synthetic browser acquisition timed out')
      await playback(page)
      const created = await api(page, '/users', 'POST', { username: 'qualification-user', password, role: 'user' })
      assert.equal(created.status, 201)
      await context.clearCookies()
      await login(page, 'qualification-user')
      assert.equal((await api(page, '/users')).status, 403)
      assert.equal((await api(page, '/settings', 'PUT', {})).status, 403)
      const csrf = (await context.cookies()).find(c => c.name === 'ytmdl_csrf')
      assert(csrf)
      const denied = await context.request.post(state.base + '/api/v1/users', {
        headers: { 'X-CSRF-Token': csrf.value },
        data: { username: 'must-not-exist', password, role: 'admin' },
      })
      assert.equal(denied.status(), 403)
      await page.goto(state.base + '/settings/server')
      await page.getByText('Seite nicht gefunden', { exact: false }).first().waitFor()
      await playback(page)
      await page.screenshot({ path: process.env.YTMDL_BROWSER_SCREENSHOT || '/tmp/ytmdl-v1-browser-chromium.png', fullPage: true })
    } else {
      await login(page, 'qualification-user')
      await playback(page)
    }
    assert.equal(errors.length, 0, 'Browser reported uncaught errors')
    console.log('PASS:', name, 'real application authentication, library and audio playback' + (name === 'chromium' ? ', setup/search/acquisition and ordinary-user denial' : ''))
  } finally {
    await browser.close()
  }
}

} catch {
  console.error('FAIL: isolated browser qualification stopped')
  process.exitCode = 1
} finally {
  // Connection state is created by the disposable Compose qualification script.
  assert(state.prefix.startsWith('ytmdl-qualification-'))
  execFileSync(state.compose[0], [...state.compose.slice(1), 'down', '--volumes', '--remove-orphans'], { cwd: state.project, stdio: 'ignore' })
}
