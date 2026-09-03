/**
 * Decides whether the user typed a search query or pasted an address.
 *
 * That is the only URL knowledge left in the frontend. Reading an address —
 * which provider owns it, which id it carries, and what a handle such as
 * youtube.com/@artist resolves to — is provider knowledge and lives behind
 * GET /api/v1/resolve, so it is not duplicated here.
 */

/** Hosts whose addresses the backend can resolve. */
const KNOWN_HOSTS = [
  'music.youtube.com',
  'www.youtube.com',
  'youtube.com',
  'm.youtube.com',
  'youtu.be',
  'open.spotify.com',
  'play.spotify.com',
  'deezer.com',
  'www.deezer.com',
]

/** A provider id pasted on its own, which the backend also accepts. */
const BARE_ID = /^(UC|MPLA|MPRE|OLAK)[A-Za-z0-9_-]{2,124}$/

/** A Spotify URI such as spotify:artist:xxxx. */
const SPOTIFY_URI = /^spotify:(artist|album|track):[A-Za-z0-9]{22}$/

/** A Deezer URI such as deezer:artist:xxxx. */
const DEEZER_URI = /^deezer:(artist|album|track):[0-9]{1,32}$/

/**
 * True when the input should be handed to the resolver rather than searched
 * for.
 *
 * Erring towards false is the safe direction: a link mistaken for a query
 * merely returns no results, while a query mistaken for a link costs a failed
 * resolve before the search ever runs.
 */
export function looksLikeAddress(input: string): boolean {
  const text = input.trim()
  if (!text) return false

  if (SPOTIFY_URI.test(text)) return true
  if (DEEZER_URI.test(text)) return true
  if (BARE_ID.test(text)) return true

  const host = hostOf(text)
  if (!host) return false

  // A subdomain of a known host counts too: music.youtube.com is reached both
  // ways depending on where the link was copied from.
  return KNOWN_HOSTS.some((known) => host === known || host.endsWith(`.${known}`))
}

/** The hostname of an address, or null when the input is not one. */
function hostOf(text: string): string | null {
  const candidate = text.includes('://') ? text : `https://${text}`
  try {
    const url = new URL(candidate)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null
    // A host without a dot is a search term, not an address.
    return url.hostname.includes('.') ? url.hostname.toLowerCase() : null
  } catch {
    return null
  }
}
