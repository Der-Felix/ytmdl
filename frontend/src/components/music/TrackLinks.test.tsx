import { describe, expect, it, mock } from 'bun:test'
import { fireEvent, render, screen } from '@testing-library/react'

import { TrackAlbumLink, TrackArtistLink } from './TrackLinks'
import type { LibraryTrack } from '@/types/api'

const track = {
  id: 't1',
  title: 'Leise Räume',
  artists: ['Nachtfalter', 'Gast'],
  album: 'Dämmerung',
  album_artist: 'Nachtfalter',
  artist_id: 'artist 1',
  release_id: 'release-1',
} as LibraryTrack

describe('track links', () => {
  it('link artist and album to the library pages when the ids are there', () => {
    render(
      <p>
        <TrackArtistLink track={track} /> <TrackAlbumLink track={track} />
      </p>,
    )
    const artist = screen.getByRole('link', { name: 'Nachtfalter · Gast' })
    expect(artist.getAttribute('href')).toBe('/library/artists/artist%201')
    expect(screen.getByRole('link', { name: 'Dämmerung' }).getAttribute('href')).toBe('/library/releases/release-1')
    expect(artist.className).toContain('focus-visible:ring-2')
  })

  it('stay plain text without ids', () => {
    render(
      <p>
        <TrackArtistLink track={{ ...track, artist_id: undefined, artists: [] }} />{' '}
        <TrackAlbumLink track={{ ...track, release_id: undefined }} />
      </p>,
    )
    expect(screen.queryAllByRole('link')).toHaveLength(0)
    expect(screen.getByText('Nachtfalter')).toBeDefined()
    expect(screen.getByText('Dämmerung')).toBeDefined()
  })

  it('do not trigger the row they sit in', () => {
    const rowClick = mock(() => {})
    const rowDouble = mock(() => {})
    render(
      <div onClick={rowClick} onDoubleClick={rowDouble}>
        <TrackArtistLink track={track} />
      </div>,
    )
    const link = screen.getByRole('link')
    fireEvent.click(link)
    fireEvent.doubleClick(link)
    expect(rowClick).not.toHaveBeenCalled()
    expect(rowDouble).not.toHaveBeenCalled()
    expect(window.location.pathname).toBe('/library/artists/artist%201')
  })
})
