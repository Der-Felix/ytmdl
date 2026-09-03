import { describe, expect, it } from 'bun:test'
import { matchRoute, paths } from './router'

describe('matchRoute', () => {
  it('matches root dashboard', () => {
    expect(matchRoute({ pathname: '/', params: new URLSearchParams(), href: '/' })).toEqual({
      name: 'dashboard',
    })
    expect(
      matchRoute({ pathname: '/dashboard', params: new URLSearchParams(), href: '/dashboard' }),
    ).toEqual({
      name: 'dashboard',
    })
  })

  it('matches discover with and without query', () => {
    expect(
      matchRoute({ pathname: '/discover', params: new URLSearchParams('q=daft'), href: '/discover?q=daft' }),
    ).toEqual({
      name: 'discover',
      query: 'daft',
    })
    expect(
      matchRoute({ pathname: '/discover', params: new URLSearchParams(), href: '/discover' }),
    ).toEqual({
      name: 'discover',
      query: '',
    })
  })

  it('matches artist route with provider', () => {
    expect(
      matchRoute({
        pathname: '/artists/UC12345',
        params: new URLSearchParams('provider=ytmusic'),
        href: '/artists/UC12345?provider=ytmusic',
      }),
    ).toEqual({
      name: 'artist',
      id: 'UC12345',
      provider: 'ytmusic',
    })
  })

  it('matches release route', () => {
    expect(
      matchRoute({
        pathname: '/releases/MPREb_123',
        params: new URLSearchParams('provider=ytmusic'),
        href: '/releases/MPREb_123?provider=ytmusic',
      }),
    ).toEqual({
      name: 'release',
      id: 'MPREb_123',
      provider: 'ytmusic',
    })
  })

  it('matches downloads route with and without jobId', () => {
    expect(
      matchRoute({ pathname: '/downloads', params: new URLSearchParams(), href: '/downloads' }),
    ).toEqual({
      name: 'downloads',
    })
    expect(
      matchRoute({
        pathname: '/downloads/job-abc-123',
        params: new URLSearchParams(),
        href: '/downloads/job-abc-123',
      }),
    ).toEqual({
      name: 'downloads',
      jobId: 'job-abc-123',
    })
  })

  it('matches library route', () => {
    expect(
      matchRoute({ pathname: '/library', params: new URLSearchParams(), href: '/library' }),
    ).toEqual({
      name: 'library',
      artistId: undefined,
      view: undefined,
      trackId: undefined,
    })
    expect(
      matchRoute({
        pathname: '/library',
        params: new URLSearchParams('artist=art-1&view=tracks&track=trk-1'),
        href: '/library?artist=art-1&view=tracks&track=trk-1',
      }),
    ).toEqual({
      name: 'library',
      artistId: 'art-1',
      view: 'tracks',
      trackId: 'trk-1',
    })
  })

  it('matches local library artist and release routes', () => {
    expect(
      matchRoute({ pathname: '/library/artists/art-123', params: new URLSearchParams(), href: '/library/artists/art-123' }),
    ).toEqual({
      name: 'libraryArtist',
      id: 'art-123',
    })
    expect(
      matchRoute({ pathname: '/library/releases/rel-456', params: new URLSearchParams(), href: '/library/releases/rel-456' }),
    ).toEqual({
      name: 'libraryRelease',
      id: 'rel-456',
    })
  })

  it('matches settings route', () => {
    expect(
      matchRoute({ pathname: '/settings/server', params: new URLSearchParams(), href: '/settings/server' }),
    ).toEqual({
      name: 'settings',
    })
  })

  it('returns notFound for unknown routes', () => {
    expect(
      matchRoute({ pathname: '/unknown/deep/path', params: new URLSearchParams(), href: '/unknown/deep/path' }),
    ).toEqual({
      name: 'notFound',
      pathname: '/unknown/deep/path',
    })
  })
})

describe('paths', () => {
  it('generates proper URLs', () => {
    expect(paths.dashboard()).toBe('/')
    expect(paths.discover()).toBe('/discover')
    expect(paths.discover('rock')).toBe('/discover?q=rock')
    expect(paths.artist('UC123', 'ytmusic')).toBe('/artists/UC123?provider=ytmusic')
    expect(paths.release('MPRE123')).toBe('/releases/MPRE123')
    expect(paths.downloads('job1')).toBe('/downloads/job1')
    expect(paths.library('art1')).toBe('/library?artist=art1')
    expect(paths.settings()).toBe('/settings/server')
  })
})

describe('subscriptions route', () => {
  it('matches the subscriptions page', () => {
    expect(
      matchRoute({
        pathname: '/subscriptions',
        params: new URLSearchParams(),
        href: '/subscriptions',
      }),
    ).toEqual({ name: 'subscriptions' })
  })

  it('does not match a deeper path', () => {
    expect(
      matchRoute({
        pathname: '/subscriptions/sub-1',
        params: new URLSearchParams(),
        href: '/subscriptions/sub-1',
      }),
    ).toEqual({ name: 'notFound', pathname: '/subscriptions/sub-1' })
  })

  it('builds the canonical path', () => {
    expect(paths.subscriptions()).toBe('/subscriptions')
  })
})
