import { describe, expect, it } from 'bun:test'

import type { libraryTracks } from '@/lib/api/library'
import { loadFilteredTracks } from '@/lib/library-play-all'
import type { LibraryTrack } from '@/types/api'

/** A fake list endpoint over `total` titles that records each page asked for. */
function fakeList(total: number) {
  const pages: { limit?: number; offset?: number; q?: string }[] = []
  const list = (async (options = {}) => {
    pages.push({ limit: options.limit, offset: options.offset, q: options.q })
    const offset = options.offset ?? 0
    const count = Math.max(0, Math.min(options.limit ?? 50, total - offset))
    const items = Array.from({ length: count }, (_, i) => ({ id: `t${offset + i}` }) as LibraryTrack)
    return { items, meta: { total } }
  }) as typeof libraryTracks
  return { list, pages }
}

describe('loadFilteredTracks', () => {
  it('reads the filtered list page by page, up to the cap', async () => {
    const { list, pages } = fakeList(1234)
    const tracks = await loadFilteredTracks({ q: 'jazz', sort: 'title', order: 'asc' }, new AbortController().signal, 250, list)
    expect(tracks).toHaveLength(250)
    expect(tracks[0]!.id).toBe('t0')
    expect(tracks[249]!.id).toBe('t249')
    expect(pages).toEqual([
      { limit: 100, offset: 0, q: 'jazz' },
      { limit: 100, offset: 100, q: 'jazz' },
      { limit: 50, offset: 200, q: 'jazz' },
    ])
  })

  it('stops at the end of a shorter list', async () => {
    const { list, pages } = fakeList(130)
    const tracks = await loadFilteredTracks({}, new AbortController().signal, 500, list)
    expect(tracks).toHaveLength(130)
    expect(pages).toHaveLength(2)
  })
})
