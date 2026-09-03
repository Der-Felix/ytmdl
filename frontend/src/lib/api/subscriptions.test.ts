import { describe, expect, it } from 'bun:test'

import { isDue, summarizeSync, syncStatusTone, unsubscribe } from './subscriptions'
import type { Subscription } from '@/types/api'

function subscription(overrides: Partial<Subscription> = {}): Subscription {
  return {
    id: 'sub-1',
    provider: 'deezer',
    artist_source_id: '27',
    artist_name: 'Daft Punk',
    enabled: true,
    auto_download: false,
    next_sync_at: new Date(Date.now() + 3600_000).toISOString(),
    last_sync_status: 'pending',
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    syncing: false,
    ...overrides,
  }
}

describe('summarizeSync', () => {
  it('says so when a run found nothing', () => {
    expect(summarizeSync({ new_releases: 0, new_tracks: 0, queued_tracks: 0 })).toBe(
      'Keine Neuigkeiten',
    )
  })

  it('uses the singular for exactly one', () => {
    expect(summarizeSync({ new_releases: 1, new_tracks: 1, queued_tracks: 1 })).toBe(
      '1 neues Release · 1 neuer Track · 1 Track in der Warteschlange',
    )
  })

  it('uses the plural beyond one', () => {
    expect(summarizeSync({ new_releases: 2, new_tracks: 12, queued_tracks: 12 })).toBe(
      '2 neue Releases · 12 neue Tracks · 12 Tracks in der Warteschlange',
    )
  })

  it('leaves the queue out when nothing was queued', () => {
    expect(summarizeSync({ new_releases: 0, new_tracks: 3, queued_tracks: 0 })).toBe(
      '3 neue Tracks',
    )
  })
})

describe('syncStatusTone', () => {
  it('maps every status onto a tone', () => {
    expect(syncStatusTone('pending')).toBe('neutral')
    expect(syncStatusTone('success')).toBe('success')
    expect(syncStatusTone('partial')).toBe('warning')
    expect(syncStatusTone('failed')).toBe('destructive')
  })
})

describe('isDue', () => {
  it('is due once the next run is in the past', () => {
    const past = new Date(Date.now() - 60_000).toISOString()
    expect(isDue(subscription({ next_sync_at: past }))).toBe(true)
  })

  it('is not due while the next run is ahead', () => {
    expect(isDue(subscription())).toBe(false)
  })

  it('is never due while the subscription is paused', () => {
    const past = new Date(Date.now() - 60_000).toISOString()
    expect(isDue(subscription({ enabled: false, next_sync_at: past }))).toBe(false)
  })

  it('treats an unreadable timestamp as not due rather than as overdue', () => {
    expect(isDue(subscription({ next_sync_at: 'not-a-date' }))).toBe(false)
  })
})

describe('unsubscribe', () => {
  it('treats 204 No Content as success rather than as a broken answer', async () => {
    const originalFetch = globalThis.fetch
    globalThis.fetch = (async () =>
      new Response(null, { status: 204 })) as unknown as typeof fetch

    try {
      // Must not throw. The backend answers a deletion with no body at all,
      // and a client that insists on a "data" envelope reports a completed
      // deletion as a failure.
      await unsubscribe('sub-1')
    } finally {
      globalThis.fetch = originalFetch
    }
  })
})
