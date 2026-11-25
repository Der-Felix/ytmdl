import { beforeEach, describe, expect, it, mock } from 'bun:test'
import { fireEvent, render, screen } from '@testing-library/react'

import type { LibraryTrack } from '@/types/api'

const playTrack = mock((..._args: unknown[]) => {})
const togglePlayPause = mock(() => {})

mock.module('@/hooks/usePlayer', () => ({
  usePlayerState: () => ({ currentTrack: null, status: 'paused', queue: [], queueIndex: -1 }),
  usePlayerActions: () => ({
    playTrack,
    togglePlayPause,
    addToQueue: () => {},
    playNext: () => {},
    startRadio: async () => true,
  }),
}))

const { TracksTable } = await import('./TracksTable')

const track = (n: number, ids = true): LibraryTrack => ({
  id: `t${n}`,
  title: `Titel ${n}`,
  artists: [`Künstler ${n}`],
  album: `Album ${n}`,
  album_artist: `Künstler ${n}`,
  ...(ids ? { artist_id: `a${n}`, release_id: `r${n}` } : {}),
  track_number: n,
  track_total: 2,
  disc_number: 1,
  disc_total: 1,
  duration_ms: 180000,
  year: 2026,
  source_provider: '',
  source_id: '',
  source_url: '',
  created_at: '',
})

const tracks = [track(1), track(2, false)]
const context = { kind: 'album' as const, id: 'r1', title: 'Album 1' }

beforeEach(() => {
  playTrack.mockClear()
  togglePlayPause.mockClear()
})

function renderTable(onTrackSelect = mock((_t: LibraryTrack) => {})) {
  render(
    <TracksTable
      tracks={tracks}
      sort="track_number"
      order="asc"
      onSortChange={() => {}}
      onTrackSelect={onTrackSelect}
      queueContext={context}
    />,
  )
  return onTrackSelect
}

describe('TracksTable rows', () => {
  it('plays from the title with the list as context, and a plain row click does nothing', () => {
    const select = renderTable()
    fireEvent.click(screen.getByRole('button', { name: 'Titel 2' }))
    expect(playTrack).toHaveBeenCalledWith(tracks[1], tracks, 1, context)
    playTrack.mockClear()
    const row = screen.getAllByRole('row')[1]!
    fireEvent.click(row.querySelectorAll('td')[6]!)
    fireEvent.click(row)
    expect(playTrack).not.toHaveBeenCalled()
    expect(select).not.toHaveBeenCalled()
  })

  it('plays on a double-click on the row', () => {
    renderTable()
    const row = screen.getAllByRole('row')[1]!
    fireEvent.doubleClick(row.querySelectorAll('td')[6]!)
    expect(playTrack).toHaveBeenCalledWith(tracks[0], tracks, 0, context)
  })

  it('links artist and album where the ids exist and opens details from the menu', async () => {
    const select = renderTable()
    expect(screen.getByRole('link', { name: 'Künstler 1' }).getAttribute('href')).toBe('/library/artists/a1')
    expect(screen.getByRole('link', { name: 'Album 1' }).getAttribute('href')).toBe('/library/releases/r1')
    expect(screen.queryByRole('link', { name: 'Künstler 2' })).toBeNull()
    expect(screen.getByText('Künstler 2')).toBeDefined()
    fireEvent.click(screen.getByRole('button', { name: 'Weitere Aktionen für Titel 1' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Details anzeigen' }))
    expect(select).toHaveBeenCalledWith(tracks[0])
    expect(playTrack).not.toHaveBeenCalled()
  })
})
