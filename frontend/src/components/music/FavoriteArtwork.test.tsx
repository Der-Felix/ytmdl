import { expect, it } from 'bun:test'
import { render, screen } from '@testing-library/react'
import { FavoriteArtwork } from './FavoriteArtwork'

it('uses distinct release covers and restores the heart for an empty collection', () => {
  const tracks = [1, 2].map((n) => ({ id: `fav-${n}`, title: `Favorite ${n}`, release_id: 'shared-release' }))
  const { rerender, container } = render(<FavoriteArtwork tracks={tracks} />)
  expect(screen.getByLabelText('Cover deiner Lieblingstitel').querySelectorAll('img').length).toBe(1)
  expect(screen.getByLabelText('Cover deiner Lieblingstitel').querySelector('img')?.getAttribute('src')).toBe('/api/v1/library/tracks/fav-1/artwork')
  rerender(<FavoriteArtwork tracks={[]} />)
  expect(screen.queryByLabelText('Cover deiner Lieblingstitel')).toBeNull()
  expect(container.querySelector('svg')).not.toBeNull()
})
