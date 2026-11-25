import { afterEach, describe, expect, it, mock } from 'bun:test'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'

import { QueueList } from './QueueList'
import { QueueOptions, SaveQueueDialog } from './QueueOptions'
import { saveQueueAsPlaylist } from '@/lib/queue-playlist'
import type { Playlist } from '@/types/playlist'
import type { QueueContext, QueueTrack } from '@/lib/audio/types'
import type { LibraryTrack } from '@/types/api'

const base: LibraryTrack = {
  id: 'x',
  title: 'X',
  artists: ['Artist'],
  album: 'Album',
  album_artist: 'Artist',
  release_id: 'rel',
  track_number: 1,
  track_total: 9,
  disc_number: 1,
  disc_total: 1,
  duration_ms: 180000,
  year: 2026,
  source_provider: 'fixture',
  source_id: '',
  source_url: '',
  created_at: '',
}
const t = (id: string, queueOrigin?: 'user' | 'autoplay'): QueueTrack => ({
  ...base,
  id,
  title: `Titel ${id}`,
  ...(queueOrigin ? { queueOrigin } : {}),
})

const setAutoplay = mock(() => {})
const reorderQueue = mock(() => {})
let state: {
  queue: QueueTrack[]
  queueIndex: number
  status: string
  queueContext: QueueContext | null
  autoplay: boolean
} = { queue: [], queueIndex: -1, status: 'playing', queueContext: null, autoplay: true }
const actions = {
  setAutoplay,
  reorderQueue,
  playQueueIndex: mock(() => {}),
  removeFromQueue: mock(() => {}),
}

mock.module('@/hooks/usePlayer', () => ({
  usePlayer: () => ({ ...state, ...actions }),
  usePlayerState: () => state,
  usePlayerActions: () => actions,
}))

const originalFetch = globalThis.fetch
afterEach(() => {
  globalThis.fetch = originalFetch
})

describe('queue sections', () => {
  it('shows what played, the current title, "Als Nächstes", the context and similar titles', () => {
    state = {
      ...state,
      queue: [t('p1'), t('c1'), t('u1', 'user'), t('u2', 'user'), t('c2'), t('r1', 'autoplay')],
      queueIndex: 1,
      queueContext: { kind: 'album', id: 'rel', title: 'Leise Räume' },
    }
    render(<QueueList />)
    const headings = screen.getAllByRole('heading', { level: 3 }).map((h) => h.textContent)
    expect(headings).toEqual(['Zuvor gespielt', 'Aktueller Titel', 'Als Nächstes', 'Weiter aus: Leise Räume', 'Ähnliche Titel'])
    const list = screen.getByRole('list', { name: 'Titel in der Warteschlange' })
    const rows = within(list).getAllByRole('button', { name: /abspielen$/ })
    expect(rows.map((r) => r.getAttribute('aria-label'))).toEqual([
      'Titel p1 abspielen',
      'Titel c1 abspielen',
      'Titel u1 abspielen',
      'Titel u2 abspielen',
      'Titel c2 abspielen',
      'Titel r1 abspielen',
    ])
    expect(rows[1]!.getAttribute('aria-current')).toBe('true')
    expect(list.querySelector('[data-queue-origin="autoplay"]')?.textContent).toContain('Titel r1')
  })

  it('uses a neutral heading when the queue has no known context', () => {
    state = { ...state, queue: [t('c1'), t('c2')], queueIndex: 0, queueContext: null }
    render(<QueueList />)
    expect(screen.getByRole('heading', { name: 'Weiter in der Warteschlange' })).toBeTruthy()
    expect(screen.queryByRole('heading', { name: 'Als Nächstes' })).toBeNull()
  })

  it('moves a title with the step buttons through the player', () => {
    reorderQueue.mockClear()
    state = { ...state, queue: [t('c1'), t('u1', 'user'), t('c2')], queueIndex: 0 }
    render(<QueueList />)
    fireEvent.click(screen.getByRole('button', { name: 'Titel Titel c2 nach oben verschieben' }))
    expect(reorderQueue).toHaveBeenCalledWith(2, 1)
  })
})

describe('queue options', () => {
  it('switches autoplay', () => {
    setAutoplay.mockClear()
    state = { ...state, queue: [t('c1')], queueIndex: 0, autoplay: true }
    render(<QueueOptions />)
    const toggle = screen.getByRole('switch', { name: /Automatisch weiterspielen/ })
    expect(toggle.getAttribute('aria-checked')).toBe('true')
    fireEvent.click(toggle)
    expect(setAutoplay).toHaveBeenCalledWith(false)
  })

  it('saves the queue as a playlist in requests of at most 100 titles', async () => {
    const queue = Array.from({ length: 150 }, (_, i) => t(`q${i}`))
    // A title queued twice is saved once.
    state = { ...state, queue: [...queue, t('q3', 'user')], queueIndex: 0, queueContext: { kind: 'mix', id: 'm', title: 'Mein Mix' } }
    // The API is handed in: other test files replace the API modules globally.
    const created: string[] = []
    const chunks: string[][] = []
    const api = {
      create: async ({ name }: { name: string }) => {
        created.push(name)
        return { id: 'p1', name } as Playlist
      },
      add: async (id: string, trackIds: string[]) => {
        expect(id).toBe('p1')
        chunks.push(trackIds)
      },
    }
    const onOpenChange = mock(() => {})
    render(
      <SaveQueueDialog open onOpenChange={onOpenChange} save={(name, trackIds) => saveQueueAsPlaylist(name, trackIds, api)} />,
    )
    const name = (await screen.findByLabelText('Name')) as HTMLInputElement
    expect(name.value).toMatch(/^Mein Mix · \d{2}\.\d{2}\.\d{4}$/)
    expect(screen.getByText('150 Titel in der Reihenfolge der Warteschlange.')).toBeTruthy()
    fireEvent.change(name, { target: { value: 'Abendrunde' } })
    fireEvent.click(screen.getByRole('button', { name: 'Speichern' }))
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(created).toEqual(['Abendrunde'])
    expect(chunks.map((c) => c.length)).toEqual([100, 50])
    expect(chunks.flat()).toEqual(queue.map((x) => x.id))
  })

  it('says how far saving got when adding titles fails after the playlist was made', async () => {
    let calls = 0
    const api = {
      create: async () => ({ id: 'p1', name: 'Liste' }) as Playlist,
      add: async () => {
        if (++calls === 2) throw new Error('Titel nicht gefunden.')
      },
    }
    const ids = Array.from({ length: 120 }, (_, i) => `q${i}`)
    await expect(saveQueueAsPlaylist('Liste', ids, api)).rejects.toThrow(
      'Die Playlist „Liste“ wurde angelegt, aber nur 100 von 120 Titeln hinzugefügt. Titel nicht gefunden.',
    )
  })

  it('asks for a name before saving', async () => {
    state = { ...state, queue: [t('c1')], queueIndex: 0 }
    let fetched = 0
    globalThis.fetch = (async () => {
      fetched++
      return new Response('{}')
    }) as typeof fetch
    render(<QueueOptions />)
    fireEvent.click(screen.getByRole('button', { name: 'Als Playlist speichern' }))
    const name = await screen.findByLabelText('Name')
    fireEvent.change(name, { target: { value: '   ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Speichern' }))
    expect(await screen.findByText('Bitte gib der Playlist einen Namen.')).toBeTruthy()
    expect(name.getAttribute('aria-invalid')).toBe('true')
    expect(fetched).toBe(0)
  })
})
