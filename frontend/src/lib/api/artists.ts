/** Artist search, artist lookup, discography and releases. */

import { request, requestList } from '@/lib/api/client'
import type {
  Artist,
  ResolvedRef,
  Release,
  ReleaseDetail,
  ReleaseFilter,
  ReleaseType,
} from '@/types/api'

/** GET /search/artists?q= */
export async function searchArtists(
  query: string,
  options: { provider?: string; signal?: AbortSignal } = {},
): Promise<Artist[]> {
  const result = await requestList<Artist>('/search/artists', {
    query: { q: query, provider: options.provider },
    signal: options.signal,
  })
  return result.items
}

/**
 * GET /resolve?url=
 *
 * Turns a pasted address into the provider identity the catalogue endpoints
 * take. The backend owns this: it knows each provider's id formats, and it is
 * the only side that can look up the canonical channel id behind a handle such
 * as youtube.com/@artist.
 *
 * An address it cannot read comes back as an ApiError with INVALID_REQUEST.
 */
export function resolveAddress(
  address: string,
  signal?: AbortSignal,
): Promise<ResolvedRef> {
  return request<ResolvedRef>('/resolve', { query: { url: address }, signal })
}

/** GET /artists/{id} */
export function getArtist(
  id: string,
  options: { provider?: string; signal?: AbortSignal } = {},
): Promise<Artist> {
  return request<Artist>(`/artists/${encodeURIComponent(id)}`, {
    query: { provider: options.provider },
    signal: options.signal,
  })
}

/**
 * GET /artists/{id}/discography
 *
 * Without a filter the backend applies its own default (albums, singles, EPs).
 * The artist page wants everything the provider knows, so it passes a filter
 * that selects every type and groups the result itself.
 */
export async function getDiscography(
  id: string,
  options: {
    provider?: string
    filter?: ReleaseFilter
    signal?: AbortSignal
  } = {},
): Promise<Release[]> {
  const filter = options.filter
  const result = await requestList<Release>(
    `/artists/${encodeURIComponent(id)}/discography`,
    {
      query: {
        provider: options.provider,
        albums: filter?.albums,
        singles: filter?.singles,
        eps: filter?.eps,
        live: filter?.live,
        compilations: filter?.compilations,
        remixes: filter?.remixes,
      },
      signal: options.signal,
    },
  )
  return result.items
}

/** GET /releases/{id} — the release including its track list. */
export function getRelease(
  id: string,
  options: { provider?: string; signal?: AbortSignal } = {},
): Promise<ReleaseDetail> {
  return request<ReleaseDetail>(`/releases/${encodeURIComponent(id)}`, {
    query: { provider: options.provider },
    signal: options.signal,
  })
}

/** Every release type, for requests that want the complete discography. */
export const ALL_RELEASE_TYPES: ReleaseFilter = {
  albums: true,
  singles: true,
  eps: true,
  live: true,
  compilations: true,
  remixes: true,
}

/** The backend's own default selection (music.DefaultReleaseFilter). */
export const DEFAULT_RELEASE_FILTER: ReleaseFilter = {
  albums: true,
  singles: true,
  eps: true,
  live: false,
  compilations: false,
  remixes: false,
}

/** The label shown for a release type, singular and plural. */
export const RELEASE_TYPE_LABELS: Record<
  ReleaseType,
  { one: string; many: string }
> = {
  album: { one: 'Album', many: 'Alben' },
  single: { one: 'Single', many: 'Singles' },
  ep: { one: 'EP', many: 'EPs' },
  live: { one: 'Live', many: 'Live' },
  compilation: { one: 'Kompilation', many: 'Kompilationen' },
  remix: { one: 'Remix', many: 'Remixe' },
}

/** The order release groups appear in on the artist page. */
export const RELEASE_TYPE_ORDER: ReleaseType[] = [
  'album',
  'ep',
  'single',
  'live',
  'compilation',
  'remix',
]

/**
 * Groups a discography by release type, newest first inside each group. Only
 * types that actually occur are returned, so the page never renders an empty
 * "Remixe" heading.
 */
export function groupReleases(
  releases: Release[],
): { type: ReleaseType; releases: Release[] }[] {
  const groups = new Map<ReleaseType, Release[]>()
  for (const release of releases) {
    const existing = groups.get(release.release_type)
    if (existing) existing.push(release)
    else groups.set(release.release_type, [release])
  }

  return RELEASE_TYPE_ORDER.filter((type) => groups.has(type)).map((type) => ({
    type,
    releases: [...(groups.get(type) ?? [])].sort(byYearDescending),
  }))
}

function byYearDescending(a: Release, b: Release): number {
  if (a.year !== b.year) return b.year - a.year
  return a.title.localeCompare(b.title, 'de')
}
