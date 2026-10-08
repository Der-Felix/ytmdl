import { expect, it } from 'bun:test'

// App.test.tsx replaces the playlist API and player hooks process-wide. Run the
// real request/interaction checks in a fresh process so those mocks cannot
// conceal failures or replace the API under test. The fixture uses only fetch
// stubs, a loopback DOM origin and never starts audio.
it('verifies playlist artwork and editing with the real client in isolation', async () => {
  const process = Bun.spawn([Bun.which('bun')!, 'test', './src/test/playlist-listening.fixture.tsx'], {
    cwd: import.meta.dir + '/../..', stdout: 'pipe', stderr: 'pipe',
  })
  const [status, stdout, stderr] = await Promise.all([
    process.exited, new Response(process.stdout).text(), new Response(process.stderr).text(),
  ])
  expect({ status, diagnostics: status ? stdout + stderr : '' }).toEqual({ status: 0, diagnostics: '' })
}, 15000)
