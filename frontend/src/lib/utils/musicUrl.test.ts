import { describe, expect, test } from 'bun:test'

import { looksLikeAddress } from '@/lib/utils/musicUrl'

/**
 * The frontend only decides search-or-resolve; reading the address itself is
 * the backend's job and is covered by internal/resolve/resolve_test.go. What
 * matters here is that every address the backend can handle actually reaches
 * it, and that ordinary search terms never do.
 */
describe('looksLikeAddress', () => {
  describe('recognises addresses the resolver handles', () => {
    const addresses = [
      // Handles — these carry no id and are what the resolver looks up.
      'https://youtube.com/@artist',
      'https://www.youtube.com/@artist',
      'https://www.youtube.com/@LinkinPark',
      'https://www.youtube.com/@LinkinPark/videos',
      'youtube.com/@artist',
      'www.youtube.com/@artist',

      // Channel addresses.
      'https://youtube.com/channel/UCxgN32UVVztKAQd2HkXzBtw',
      'https://www.youtube.com/channel/UCxgN32UVVztKAQd2HkXzBtw',
      'https://music.youtube.com/channel/UCxgN32UVVztKAQd2HkXzBtw',
      'music.youtube.com/channel/UCxgN32UVVztKAQd2HkXzBtw',

      // Legacy channel addresses.
      'https://www.youtube.com/c/LinkinPark',
      'https://www.youtube.com/user/LinkinPark',

      // Releases.
      'https://music.youtube.com/browse/MPREb_d1UkStdzUrN',
      'https://music.youtube.com/playlist?list=OLAK5uy_abcdefghijkl',

      // Spotify.
      'https://open.spotify.com/artist/6XyY86QOPPrYVGvF9ch6wz',
      'https://open.spotify.com/intl-de/album/6XyY86QOPPrYVGvF9ch6wz',
      'spotify:artist:6XyY86QOPPrYVGvF9ch6wz',

      // Deezer.
      'https://www.deezer.com/artist/27',
      'https://www.deezer.com/de/album/6575789',
      'https://deezer.com/track/67238732',
      'deezer:artist:27',
      'deezer:album:6575789',
      'deezer:track:67238732',

      // Bare provider ids.
      'UCxgN32UVVztKAQd2HkXzBtw',
      'MPREb_d1UkStdzUrN',
      'OLAK5uy_abcdefghijkl',

      // Addresses the resolver rejects with an explanation still have to reach
      // it, so that the user is told why rather than shown empty search results.
      'https://www.youtube.com/watch?v=dQw4w9WgXcQ',
      'https://youtu.be/dQw4w9WgXcQ',
    ]

    test.each(addresses)('%s', (address) => {
      expect(looksLikeAddress(address)).toBe(true)
    })
  })

  describe('treats search terms as queries', () => {
    const queries = [
      'Linkin Park',
      'Daft Punk',
      'Lacazette',
      'Meteora',
      '  ',
      '',
      'AC/DC',
      'https://Linkin Park',
      // Not a known host: searching is more useful than a failed resolve.
      'https://soundcloud.com/artist',
      'example.com/artist',
      // Looks id-ish but is not a provider id.
      'UC',
      'MPRE',
      'nirvana',
    ]

    test.each(queries)('%p', (query) => {
      expect(looksLikeAddress(query)).toBe(false)
    })
  })

  test('ignores surrounding whitespace', () => {
    expect(looksLikeAddress('  https://youtube.com/@artist  ')).toBe(true)
  })

  test('rejects non-http schemes', () => {
    expect(looksLikeAddress('ftp://youtube.com/channel/UCabc')).toBe(false)
    expect(looksLikeAddress('javascript:alert(1)')).toBe(false)
  })
})
