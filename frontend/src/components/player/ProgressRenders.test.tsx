import { expect, it } from 'bun:test'

// Counts how often the mini player, the queue and the player page redraw while
// a track plays. Other test files replace the player hooks process-wide, so the
// measurement runs in a fresh process, like PlaylistListening.test.tsx.
it('keeps the player views still while only the position changes', async () => {
  const process = Bun.spawn([Bun.which('bun')!, 'test', './src/test/progress-renders.fixture.tsx'], {
    cwd: import.meta.dir + '/../../..',
    stdout: 'pipe',
    stderr: 'pipe',
  })
  const [status, stdout, stderr] = await Promise.all([
    process.exited,
    new Response(process.stdout).text(),
    new Response(process.stderr).text(),
  ])
  expect({ status, diagnostics: status ? stdout + stderr : '' }).toEqual({ status: 0, diagnostics: '' })
}, 20000)
