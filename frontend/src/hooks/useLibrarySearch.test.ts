import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { act, renderHook, waitFor } from '@testing-library/react'

import type { LibrarySearchResults } from '@/types/api'
import { useLibrarySearch } from './useLibrarySearch'

const originalFetch = globalThis.fetch
let calls: { query: string; signal?: AbortSignal | null }[] = []
let pending = new Map<string, (results: LibrarySearchResults) => void>()

function answer(title: string): LibrarySearchResults {
  return { artists: [], releases: [], tracks: [{ id: title, title } as LibrarySearchResults['tracks'][number]] }
}

beforeEach(() => {
  calls = []
  pending = new Map()
  globalThis.fetch = ((input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(typeof input === 'string' ? input : input.toString(), 'http://localhost')
    const query = url.searchParams.get('q') ?? ''
    calls.push({ query, signal: init?.signal })
    return new Promise<Response>((resolve) => {
      pending.set(query, (results) =>
        resolve(new Response(JSON.stringify({ data: results }), { status: 200, headers: { 'Content-Type': 'application/json' } })),
      )
    })
  }) as typeof fetch
})

afterEach(() => {
  globalThis.fetch = originalFetch
})

const wait = (ms: number) => act(() => new Promise((resolve) => setTimeout(resolve, ms)))

describe('useLibrarySearch', () => {
  it('waits for the typing to pause and asks once with the limit', async () => {
    const { result } = renderHook(() => useLibrarySearch({ debounceMs: 40, limit: 5 }))
    act(() => result.current.search('k'))
    act(() => result.current.search('kr'))
    act(() => result.current.search('kra'))
    expect(calls).toHaveLength(0)
    await wait(60)
    expect(calls.map((call) => call.query)).toEqual(['kra'])
    expect(result.current.isLoading).toBe(true)

    await act(async () => pending.get('kra')?.(answer('Computerwelt')))
    await waitFor(() => expect(result.current.results?.tracks[0]?.title).toBe('Computerwelt'))
    expect(result.current.resultQuery).toBe('kra')
    expect(result.current.isLoading).toBe(false)
  })

  it('asks nothing below the minimum length and drops what it had', async () => {
    const { result } = renderHook(() => useLibrarySearch({ debounceMs: 10, minLength: 2 }))
    act(() => result.current.search('ab'))
    await wait(20)
    await act(async () => pending.get('ab')?.(answer('Abba')))
    await waitFor(() => expect(result.current.results).not.toBeNull())

    act(() => result.current.search('a'))
    await wait(20)
    expect(calls).toHaveLength(1)
    expect(result.current.results).toBeNull()
  })

  it('aborts the running request at the next keystroke and never lets it land', async () => {
    const seen: string[] = []
    const { result } = renderHook(() =>
      useLibrarySearch({ debounceMs: 10, onResults: (_, query) => seen.push(query) }),
    )
    act(() => result.current.search('first'))
    await wait(20)
    act(() => result.current.search('second'))
    expect(calls[0]?.signal?.aborted).toBe(true)
    await wait(20)

    await act(async () => pending.get('second')?.(answer('Neu')))
    await waitFor(() => expect(result.current.results?.tracks[0]?.title).toBe('Neu'))
    await act(async () => pending.get('first')?.(answer('Alt')))
    await wait(10)
    expect(result.current.results?.tracks[0]?.title).toBe('Neu')
    expect(seen).toEqual(['second'])
  })

  it('keeps the old results while a new query is on its way', async () => {
    const { result } = renderHook(() => useLibrarySearch({ debounceMs: 10 }))
    act(() => result.current.search('kra'))
    await wait(20)
    await act(async () => pending.get('kra')?.(answer('Computerwelt')))
    await waitFor(() => expect(result.current.results).not.toBeNull())

    act(() => result.current.search('kraft'))
    await wait(20)
    expect(result.current.isLoading).toBe(true)
    expect(result.current.results?.tracks[0]?.title).toBe('Computerwelt')
    expect(result.current.resultQuery).toBe('kra')
  })

  it('clear cancels a pending search', async () => {
    const { result } = renderHook(() => useLibrarySearch({ debounceMs: 10 }))
    act(() => result.current.search('kra'))
    act(() => result.current.clear())
    await wait(30)
    expect(calls).toHaveLength(0)
    expect(result.current.results).toBeNull()
  })
})
