import { beforeEach, describe, expect, it, mock } from 'bun:test'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { Trash2 } from 'lucide-react'

import type { LibraryTrack } from '@/types/api'

const playNext = mock((_track: LibraryTrack) => {})
const addToQueue = mock((_track: LibraryTrack) => {})
const startRadio = mock(async (_source: { seed?: LibraryTrack }) => true)
const toggleFavorite = mock(async (_id: string) => {})
let favoriteIds = new Set<string>()

mock.module('@/hooks/usePlayer', () => ({
  usePlayerState: () => ({ currentTrack: null, status: 'paused', queue: [], queueIndex: -1 }),
  usePlayerActions: () => ({ playNext, addToQueue, startRadio }),
}))
mock.module('@/hooks/useFavorites', () => ({
  useOptionalFavorites: () => ({ isFavorite: (id: string) => favoriteIds.has(id), toggleFavorite }),
}))

const { TrackMenu } = await import('./TrackMenu')

const track: LibraryTrack = {
  id: 't1',
  title: 'Leise Räume',
  artists: ['Nachtfalter'],
  album: 'Dämmerung',
  album_artist: 'Nachtfalter',
  artist_id: 'artist-1',
  release_id: 'release-1',
  track_number: 1,
  track_total: 9,
  disc_number: 1,
  disc_total: 1,
  duration_ms: 180000,
  year: 2026,
  source_provider: '',
  source_id: '',
  source_url: '',
  created_at: '',
}

async function openMenu() {
  fireEvent.click(screen.getByRole('button', { name: 'Weitere Aktionen für Leise Räume' }))
  await screen.findByRole('menu')
}

function entries() {
  return screen
    .getAllByRole('menuitem')
    .map((item) => item.textContent?.trim())
}

beforeEach(() => {
  favoriteIds = new Set()
  window.history.replaceState(null, '', '/library')
  for (const fn of [playNext, addToQueue, startRadio, toggleFavorite]) fn.mockClear()
})

describe('TrackMenu', () => {
  it('lists every entry that applies to a library track', async () => {
    render(<TrackMenu track={track} onShowDetails={() => {}} />)
    await openMenu()
    expect(entries()).toEqual([
      'Als Nächstes abspielen',
      'Zur Warteschlange hinzufügen',
      'Song-Radio starten',
      'Zu Playlist hinzufügen …',
      'Zu Favoriten hinzufügen',
      'Zum Künstler',
      'Zum Album',
      'Details anzeigen',
    ])
    expect(screen.getByRole('menuitem', { name: 'Zum Künstler' }).getAttribute('href')).toBe('/library/artists/artist-1')
    expect(screen.getByRole('menuitem', { name: 'Zum Album' }).getAttribute('href')).toBe('/library/releases/release-1')
  })

  it('leaves out what does not apply', async () => {
    favoriteIds = new Set(['t1'])
    const bare = { ...track, artist_id: undefined, release_id: undefined }
    render(<TrackMenu track={bare} inQueue />)
    await openMenu()
    expect(entries()).toEqual(['Song-Radio starten', 'Zu Playlist hinzufügen …', 'Aus Favoriten entfernen'])
  })

  it('hides the link to the page it is already on and radio for a browser-only copy', async () => {
    window.history.replaceState(null, '', '/library/releases/release-1')
    render(<TrackMenu track={{ ...track, offline_blob_key: 'blob' }} />)
    await openMenu()
    const names = entries()
    expect(names).toContain('Zum Künstler')
    expect(names).not.toContain('Zum Album')
    expect(names).not.toContain('Song-Radio starten')
    expect(names).not.toContain('Zu Playlist hinzufügen …')
  })

  it('runs the actions', async () => {
    const remove = mock(() => {})
    const details = mock(() => {})
    render(
      <TrackMenu
        track={track}
        onShowDetails={details}
        extras={[{ id: 'remove', label: 'Aus Playlist entfernen', icon: Trash2, onSelect: remove, destructive: true }]}
      />,
    )
    const pick = async (name: string) => {
      await openMenu()
      fireEvent.click(screen.getByRole('menuitem', { name }))
      await waitFor(() => expect(screen.queryByRole('menu')).toBeNull())
    }
    await pick('Als Nächstes abspielen')
    expect(playNext).toHaveBeenCalledWith(track)
    await pick('Zur Warteschlange hinzufügen')
    expect(addToQueue).toHaveBeenCalledWith(track)
    await pick('Song-Radio starten')
    expect(startRadio).toHaveBeenCalledWith({ seed: track })
    await pick('Zu Favoriten hinzufügen')
    expect(toggleFavorite).toHaveBeenCalledWith('t1')
    await pick('Details anzeigen')
    expect(details).toHaveBeenCalled()
    await pick('Aus Playlist entfernen')
    expect(remove).toHaveBeenCalled()
  })

  it('opens from a right-click on its row, but not on a link in it', async () => {
    render(
      <ul>
        <li data-track-row>
          <span>Leise Räume</span>
          <a href="/library/artists/artist-1">Nachtfalter</a>
          <TrackMenu track={track} />
        </li>
      </ul>,
    )
    fireEvent.contextMenu(screen.getByText('Nachtfalter'), { button: 2, clientX: 40, clientY: 40 })
    expect(screen.queryByRole('menu')).toBeNull()
    fireEvent.contextMenu(screen.getByText('Leise Räume'), { button: 2, clientX: 40, clientY: 40 })
    expect(await screen.findByRole('menu')).toBeDefined()
  })

  it('does not pass its button click on to the row', () => {
    const rowClick = mock(() => {})
    render(
      <div onClick={rowClick}>
        <TrackMenu track={track} />
      </div>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'Weitere Aktionen für Leise Räume' }))
    expect(rowClick).not.toHaveBeenCalled()
  })
})
