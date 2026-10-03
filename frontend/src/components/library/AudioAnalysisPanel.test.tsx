import { beforeEach, afterEach, it, expect } from 'bun:test'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { AudioAnalysisPanel } from './AudioAnalysisPanel'
let saved: typeof fetch,
  analyzed: number,
  reset: number,
  hold: boolean,
  aborted: boolean
beforeEach(() => {
  saved = globalThis.fetch
  analyzed = 0
  reset = 0
  hold = false
  aborted = false
  globalThis.fetch = (async (_input: RequestInfo | URL, init?: RequestInit) => {
    if (init?.method === 'DELETE') {
      reset++
      return new Response(null, { status: 204 })
    }
    if (init?.method === 'POST') {
      analyzed++
      if (hold)
        await new Promise((_resolve, reject) =>
          init.signal?.addEventListener('abort', () => {
            aborted = true
            reject(new DOMException('Canceled', 'AbortError'))
          }),
        )
      return new Response(JSON.stringify({ data: { state: 'ready' } }))
    }
    return new Response(
      JSON.stringify({
        data: {
          pending: analyzed ? 0 : 1,
          ready: analyzed,
          inconclusive: 0,
          failed: 0,
          next_ids: analyzed ? [] : ['track-fixture'],
        },
      }),
    )
  }) as typeof fetch
})
afterEach(() => {
  globalThis.fetch = saved
})
it('requires explicit start and resets only after confirmation', async () => {
  render(<AudioAnalysisPanel />)
  await screen.findByText(/0 erkannt/)
  expect(analyzed).toBe(0)
  fireEvent.click(
    screen.getByRole('button', { name: 'Audioerkennung starten' }),
  )
  await screen.findByText('1 Titel in diesem Durchlauf geprüft.')
  expect(analyzed).toBe(1)
  fireEvent.click(
    screen.getByRole('button', { name: 'Analyseindex zurücksetzen' }),
  )
  expect(reset).toBe(0)
  fireEvent.click(
    screen.getByRole('button', { name: 'Index zurücksetzen', exact: true }),
  )
  await waitFor(() => expect(reset).toBe(1))
})
it('cancel aborts the current request without hiding a server error', async () => {
  hold = true
  render(<AudioAnalysisPanel />)
  await screen.findByText(/0 erkannt/)
  fireEvent.click(
    screen.getByRole('button', { name: 'Audioerkennung starten' }),
  )
  await waitFor(() => expect(analyzed).toBe(1))
  fireEvent.click(
    screen.getByRole('button', { name: 'Abbrechen', exact: true }),
  )
  await waitFor(() => expect(aborted).toBe(true))
  expect(screen.queryByRole('alert')).toBeNull()
})
