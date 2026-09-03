import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { StoragePanel } from './StoragePanel'
import type { StorageStatusResponse } from '@/types/api'

const sampleStorageData: StorageStatusResponse = {
  library: {
    path: '/music',
    guard_configured: true,
    guard_status: 'verified',
    status: 'healthy',
    fs_type: 'NFS',
    total_bytes: 1099511627776,
    free_bytes: 549755813888,
    used_bytes: 549755813888,
    free_percent: 50.0,
    min_free_bytes: 1073741824,
    is_network_fs: true,
    last_checked_at: new Date().toISOString(),
  },
  staging: {
    path: '/data/staging',
    total_bytes: 107374182400,
    free_bytes: 85899345920,
    used_bytes: 21474836480,
    min_free_bytes: 2147483648,
    max_bytes: 10737418240,
    current_staged_bytes: 52428800,
    active_items: 1,
    active_partials: 2,
  },
  queue: {
    paused: false,
    waiting_storage_items: 0,
    waiting_space_items: 0,
    retry_wait_items: 0,
  },
}

let originalFetch: typeof fetch

beforeEach(() => {
  originalFetch = globalThis.fetch
})

afterEach(() => {
  globalThis.fetch = originalFetch
})

describe('StoragePanel', () => {
  it('renders library and staging metrics correctly', () => {
    render(<StoragePanel initialData={sampleStorageData} />)

    expect(screen.getByText('Musik-Bibliothek')).toBeDefined()
    expect(screen.getByText('/music')).toBeDefined()
    expect(screen.getByText('Bereit & Beschreibbar')).toBeDefined()
    expect(screen.getByText('NFS')).toBeDefined()

    expect(screen.getByText('Persistentes Staging')).toBeDefined()
    expect(screen.getByText('/data/staging')).toBeDefined()
    expect(screen.getByText('2')).toBeDefined() // active partials
  })

  it('triggers storage probe when clicking button', async () => {
    let probeCalled = false

    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      if (url.includes('/storage/probe')) {
        probeCalled = true
        return new Response(JSON.stringify({ data: sampleStorageData }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      return new Response(JSON.stringify({ data: {} }), { status: 200 })
    }) as unknown as typeof fetch

    render(<StoragePanel initialData={sampleStorageData} />)

    const probeBtn = screen.getByRole('button', { name: /Jetzt prüfen/i })
    fireEvent.click(probeBtn)

    await waitFor(() => {
      expect(probeCalled).toBe(true)
    })
  })

  it('toggles pause and resume queue', async () => {
    let pauseCalled = false

    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      if (url.includes('/storage/queue/pause')) {
        pauseCalled = true
        return new Response(JSON.stringify({ data: { paused: true } }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      if (url.includes('/storage/status')) {
        return new Response(
          JSON.stringify({
            data: {
              ...sampleStorageData,
              queue: { ...sampleStorageData.queue, paused: true },
            },
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        )
      }
      return new Response(JSON.stringify({ data: {} }), { status: 200 })
    }) as unknown as typeof fetch

    render(<StoragePanel initialData={sampleStorageData} />)

    const pauseBtn = screen.getByRole('button', { name: /Warteschlange anhalten/i })
    fireEvent.click(pauseBtn)

    await waitFor(() => {
      expect(pauseCalled).toBe(true)
    })
  })
})
