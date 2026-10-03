import { afterEach, expect, test } from 'bun:test'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { DevicePairing } from './DevicePairing'

const originalFetch = globalThis.fetch
const setURL = (url: string) => (window as unknown as { happyDOM: { setURL(url: string): void } }).happyDOM.setURL(url)
afterEach(() => {
  cleanup()
  globalThis.fetch = originalFetch
  window.history.replaceState(null, '', '/')
  setURL('http://localhost/')
})

test('a QR code only prefills: preview and explicit confirmation are separate', async () => {
  setURL('http://localhost/profile?device_code=ABCD-EFGH')
  document.cookie = 'ytmdl_csrf=fixture-csrf; path=/'
  const calls: string[] = []
  globalThis.fetch = (async (input, init) => {
    calls.push(String(input))
    expect(init?.method).toBe('POST')
    if (String(input).endsWith('/preview')) return new Response(JSON.stringify({ data: { device_name: 'Apple TV', expires_at: '2026-10-04T12:00:00Z' } }), { headers: { 'Content-Type': 'application/json' } })
    return new Response(null, { status: 204 })
  }) as typeof fetch
  render(<DevicePairing />)
  expect((screen.getByLabelText('Code vom gewünschten Gerät') as HTMLInputElement).value).toBe('ABCD-EFGH')
  expect(calls).toHaveLength(0)
  fireEvent.click(screen.getByRole('button', { name: 'Gerät prüfen' }))
  await screen.findByText('Apple TV anmelden?')
  expect(calls).toHaveLength(1)
  fireEvent.click(screen.getByRole('button', { name: 'Dieses Gerät ausdrücklich freigeben' }))
  await screen.findByRole('status')
  expect(calls).toHaveLength(2)
  expect(window.location.search).toBe('')
})

test('changing a previewed code removes the approval action', async () => {
  document.cookie = 'ytmdl_csrf=fixture-csrf; path=/'
  globalThis.fetch = (async () => new Response(JSON.stringify({ data: { device_name: 'Apple TV', expires_at: '2026-10-04T12:00:00Z' } }), { headers: { 'Content-Type': 'application/json' } })) as typeof fetch
  render(<DevicePairing />)
  const input = screen.getByLabelText('Code vom gewünschten Gerät')
  fireEvent.change(input, { target: { value: 'ABCD-EFGH' } })
  fireEvent.click(screen.getByRole('button', { name: 'Gerät prüfen' }))
  await screen.findByText('Apple TV anmelden?')
  await waitFor(() => expect((input as HTMLInputElement).disabled).toBe(false))
  fireEvent.change(input, { target: { value: 'WXYZ-2345' } })
  expect(screen.queryByRole('button', { name: 'Dieses Gerät ausdrücklich freigeben' })).toBeNull()
})
