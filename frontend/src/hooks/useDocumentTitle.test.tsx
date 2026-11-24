import { afterEach, describe, expect, it } from 'bun:test'
import { cleanup, render } from '@testing-library/react'

import { documentTitle, useDocumentTitle, usePageTitle } from './useDocumentTitle'
import type { TitleTrack } from './useDocumentTitle'

const track: TitleTrack = { title: 'Moon Safari', artists: ['Air'], album_artist: 'Air' }

function Shell({ page, playing, children }: { page: string; playing?: TitleTrack | null; children?: React.ReactNode }) {
  useDocumentTitle(page, playing ?? null)
  return <>{children}</>
}

function Detail({ name }: { name: string | null }) {
  usePageTitle(name)
  return null
}

afterEach(() => {
  cleanup()
  document.title = ''
})

describe('documentTitle', () => {
  it('names the page while nothing plays', () => {
    expect(documentTitle({ pageName: 'Bibliothek' })).toBe('Bibliothek – YTMDL')
    expect(documentTitle({ pageName: '' })).toBe('YTMDL')
  })

  it('names the track and its artists while playing', () => {
    expect(documentTitle({ pageName: 'Bibliothek', playingTrack: track })).toBe('Moon Safari · Air')
    expect(
      documentTitle({ pageName: 'x', playingTrack: { title: 'Duet', artists: ['A', 'B'], album_artist: '' } }),
    ).toMatch(/^Duet · A.+B$/)
    expect(documentTitle({ pageName: 'x', playingTrack: { title: 'Solo', artists: [], album_artist: 'Band' } })).toBe(
      'Solo · Band',
    )
  })
})

describe('useDocumentTitle', () => {
  it('uses the route name, then the entity name of a detail page', () => {
    const view = render(<Shell page="Künstler" />)
    expect(document.title).toBe('Künstler – YTMDL')

    view.rerender(
      <Shell page="Künstler">
        <Detail name={null} />
      </Shell>,
    )
    expect(document.title).toBe('Künstler – YTMDL')

    view.rerender(
      <Shell page="Künstler">
        <Detail name="Daft Punk" />
      </Shell>,
    )
    expect(document.title).toBe('Daft Punk – YTMDL')

    // Leaving the detail page drops its name again.
    view.rerender(<Shell page="Bibliothek" />)
    expect(document.title).toBe('Bibliothek – YTMDL')
  })

  it('prefers the playing track over the page', () => {
    const view = render(
      <Shell page="Playlist" playing={track}>
        <Detail name="Abendmix" />
      </Shell>,
    )
    expect(document.title).toBe('Moon Safari · Air')

    view.rerender(
      <Shell page="Playlist" playing={null}>
        <Detail name="Abendmix" />
      </Shell>,
    )
    expect(document.title).toBe('Abendmix – YTMDL')
  })
})
