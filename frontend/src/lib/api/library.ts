import { request, requestList, type ListResult } from '@/lib/api/client'
import type {
  AuditFinding,
  AuditRun,
  BackfillResult,
  CompatReport,
  Job,
  LibraryArtist,
  LibraryArtistDetail,
  LibraryRelease,
  LibraryReleaseDetail,
  LibrarySearchResults,
  LibraryStats,
  LibraryTrack,
  LibraryTrackDetail,
  ReorganizeRequest,
  ReorganizeResult,
  RepairApplyRequest,
  RepairApplyResult,
  RepairPreview,
  ScanResult,
  TrackLyrics,
} from '@/types/api'

/** GET /library/artists */
export async function libraryArtists(
  options: {
    q?: string
    sort?: string
    order?: string
    limit?: number
    offset?: number
    signal?: AbortSignal
  } = {},
): Promise<ListResult<LibraryArtist>> {
  return requestList<LibraryArtist>('/library/artists', {
    query: {
      q: options.q,
      sort: options.sort,
      order: options.order,
      limit: options.limit,
      offset: options.offset,
    },
    signal: options.signal,
  })
}

/** GET /library/artists/{id} */
export async function libraryArtistDetail(
  id: string,
  options: { signal?: AbortSignal } = {},
): Promise<LibraryArtistDetail> {
  return request<LibraryArtistDetail>(`/library/artists/${encodeURIComponent(id)}`, {
    signal: options.signal,
  })
}

/** GET /library/releases — optionally restricted to one artist. */
export async function libraryReleases(
  options: {
    q?: string
    artistId?: string
    releaseType?: string
    year?: number
    sort?: string
    order?: string
    limit?: number
    offset?: number
    signal?: AbortSignal
  } = {},
): Promise<ListResult<LibraryRelease>> {
  return requestList<LibraryRelease>('/library/releases', {
    query: {
      q: options.q,
      artist_id: options.artistId,
      release_type: options.releaseType,
      year: options.year,
      sort: options.sort,
      order: options.order,
      limit: options.limit,
      offset: options.offset,
    },
    signal: options.signal,
  })
}

/** GET /library/releases/{id} */
export async function libraryReleaseDetail(
  id: string,
  options: { signal?: AbortSignal } = {},
): Promise<LibraryReleaseDetail> {
  return request<LibraryReleaseDetail>(`/library/releases/${encodeURIComponent(id)}`, {
    signal: options.signal,
  })
}

/** GET /library/tracks — optionally restricted to one release. */
export async function libraryTracks(
  options: {
    q?: string
    artistId?: string
    releaseId?: string
    lyricsState?: string
    sort?: string
    order?: string
    limit?: number
    offset?: number
    signal?: AbortSignal
  } = {},
): Promise<ListResult<LibraryTrack>> {
  return requestList<LibraryTrack>('/library/tracks', {
    query: {
      q: options.q,
      artist_id: options.artistId,
      release_id: options.releaseId,
      lyrics_state: options.lyricsState,
      sort: options.sort,
      order: options.order,
      limit: options.limit,
      offset: options.offset,
    },
    signal: options.signal,
  })
}

/** GET /library/tracks/{id} */
export async function libraryTrackDetail(
  id: string,
  options: { signal?: AbortSignal } = {},
): Promise<LibraryTrackDetail> {
  return request<LibraryTrackDetail>(`/library/tracks/${encodeURIComponent(id)}`, {
    signal: options.signal,
  })
}

/** GET /library/search */
export async function librarySearch(
  q: string,
  options: { limit?: number; signal?: AbortSignal } = {},
): Promise<LibrarySearchResults> {
  return request<LibrarySearchResults>('/library/search', {
    query: {
      q,
      limit: options.limit,
    },
    signal: options.signal,
  })
}

/** GET /library/stats */
export async function libraryStats(
  options: { signal?: AbortSignal } = {},
): Promise<LibraryStats> {
  return request<LibraryStats>('/library/stats', {
    signal: options.signal,
  })
}

/** POST /library/scan */
export async function startLibraryScan(
  options: { signal?: AbortSignal } = {},
): Promise<ScanResult> {
  return request<ScanResult>('/library/scan', {
    method: 'POST',
    signal: options.signal,
  })
}

/** GET /library/scan */
export async function getLibraryScan(
  options: {
    status?: string
    limit?: number
    offset?: number
    signal?: AbortSignal
  } = {},
): Promise<ScanResult> {
  return request<ScanResult>('/library/scan', {
    query: {
      status: options.status,
      limit: options.limit,
      offset: options.offset,
    },
    signal: options.signal,
  })
}

/** POST /library/tracks/{id}/redownload */
export async function redownloadLibraryTrack(
  trackId: string,
  options: { signal?: AbortSignal } = {},
): Promise<Job> {
  return request<Job>(`/library/tracks/${encodeURIComponent(trackId)}/redownload`, {
    method: 'POST',
    signal: options.signal,
  })
}

/** POST /library/tracks/{id}/retag */
export async function retagLibraryTrack(
  trackId: string,
  options: { signal?: AbortSignal } = {},
): Promise<void> {
  await request<void>(`/library/tracks/${encodeURIComponent(trackId)}/retag`, {
    method: 'POST',
    signal: options.signal,
  })
}

/** DELETE /library/tracks/{id} */
export async function deleteLibraryTrack(
  trackId: string,
  options: { signal?: AbortSignal } = {},
): Promise<void> {
  await request<void>(`/library/tracks/${encodeURIComponent(trackId)}`, {
    method: 'DELETE',
    signal: options.signal,
  })
}

/** DELETE /library/releases/{id} */
export async function deleteLibraryRelease(
  releaseId: string,
  options: { signal?: AbortSignal } = {},
): Promise<void> {
  await request<void>(`/library/releases/${encodeURIComponent(releaseId)}`, {
    method: 'DELETE',
    signal: options.signal,
  })
}

/** DELETE /library/scan/issues/{id} */
export async function deleteLibraryOrphanIssue(
  issueId: string,
  options: { signal?: AbortSignal } = {},
): Promise<void> {
  await request<void>(`/library/scan/issues/${encodeURIComponent(issueId)}`, {
    method: 'DELETE',
    signal: options.signal,
  })
}

/** GET /library/tracks/{id}/lyrics */
export async function trackLyrics(
  trackId: string,
  options: { signal?: AbortSignal } = {},
): Promise<TrackLyrics> {
  return request<TrackLyrics>(`/library/tracks/${encodeURIComponent(trackId)}/lyrics`, {
    signal: options.signal,
  })
}

/** POST /library/tracks/{id}/lyrics/refresh */
export async function refreshTrackLyrics(
  trackId: string,
  options: { signal?: AbortSignal } = {},
): Promise<TrackLyrics> {
  return request<TrackLyrics>(`/library/tracks/${encodeURIComponent(trackId)}/lyrics/refresh`, {
    method: 'POST',
    signal: options.signal,
  })
}

/** DELETE /library/tracks/{id}/lyrics */
export async function deleteTrackLyrics(
  trackId: string,
  options: { signal?: AbortSignal } = {},
): Promise<void> {
  await request<void>(`/library/tracks/${encodeURIComponent(trackId)}/lyrics`, {
    method: 'DELETE',
    signal: options.signal,
  })
}

/** POST /library/lyrics/backfill */
export async function startLyricsBackfill(
  options: { signal?: AbortSignal } = {},
): Promise<BackfillResult> {
  return request<BackfillResult>('/library/lyrics/backfill', {
    method: 'POST',
    signal: options.signal,
  })
}

/** GET /library/lyrics/backfill */
export async function getLyricsBackfillStatus(
  options: { signal?: AbortSignal } = {},
): Promise<BackfillResult> {
  return request<BackfillResult>('/library/lyrics/backfill', {
    signal: options.signal,
  })
}

/** GET /library/compatibility */
export async function getCompatibilityReport(
  options: { signal?: AbortSignal } = {},
): Promise<CompatReport> {
  return request<CompatReport>('/library/compatibility', {
    signal: options.signal,
  })
}

/** POST /library/reorganize */
export async function reorganizeLibrary(
  data: ReorganizeRequest,
  options: { signal?: AbortSignal } = {},
): Promise<ReorganizeResult> {
  return request<ReorganizeResult>('/library/reorganize', {
    method: 'POST',
    body: data,
    signal: options.signal,
  })
}

/* -------------------------------------------------------- library audit & repair */

/** POST /library/audits */
export async function startLibraryAudit(
  mode: 'quick' | 'deep' = 'quick',
  options: { signal?: AbortSignal } = {},
): Promise<AuditRun> {
  return request<AuditRun>('/library/audits', {
    method: 'POST',
    body: { mode },
    signal: options.signal,
  })
}

/** GET /library/audits */
export async function listLibraryAudits(
  options: {
    limit?: number
    offset?: number
    signal?: AbortSignal
  } = {},
): Promise<ListResult<AuditRun>> {
  return requestList<AuditRun>('/library/audits', {
    query: {
      limit: options.limit,
      offset: options.offset,
    },
    signal: options.signal,
  })
}

/** GET /library/audits/current */
export async function getCurrentLibraryAudit(
  options: { signal?: AbortSignal } = {},
): Promise<AuditRun | null> {
  return request<AuditRun | null>('/library/audits/current', {
    signal: options.signal,
  })
}

/** GET /library/audits/{id} */
export async function getLibraryAudit(
  id: string,
  options: { signal?: AbortSignal } = {},
): Promise<AuditRun> {
  return request<AuditRun>(`/library/audits/${encodeURIComponent(id)}`, {
    signal: options.signal,
  })
}

/** GET /library/audits/{id}/findings */
export async function listLibraryAuditFindings(
  id: string,
  options: {
    severity?: string
    findingCode?: string
    artistId?: string
    releaseId?: string
    trackId?: string
    limit?: number
    offset?: number
    signal?: AbortSignal
  } = {},
): Promise<ListResult<AuditFinding>> {
  return requestList<AuditFinding>(`/library/audits/${encodeURIComponent(id)}/findings`, {
    query: {
      severity: options.severity,
      finding_code: options.findingCode,
      artist_id: options.artistId,
      release_id: options.releaseId,
      track_id: options.trackId,
      limit: options.limit,
      offset: options.offset,
    },
    signal: options.signal,
  })
}

/** POST /library/audits/{id}/cancel */
export async function cancelLibraryAudit(
  id: string,
  options: { signal?: AbortSignal } = {},
): Promise<{ status: string }> {
  return request<{ status: string }>(`/library/audits/${encodeURIComponent(id)}/cancel`, {
    method: 'POST',
    signal: options.signal,
  })
}

/** POST /library/repairs/preview */
export async function previewLibraryRepairs(
  findingIds: string[],
  options: { signal?: AbortSignal } = {},
): Promise<RepairPreview[]> {
  return request<RepairPreview[]>('/library/repairs/preview', {
    method: 'POST',
    body: { finding_ids: findingIds },
    signal: options.signal,
  })
}

/** POST /library/repairs/apply */
export async function applyLibraryRepairs(
  req: RepairApplyRequest,
  options: { signal?: AbortSignal } = {},
): Promise<RepairApplyResult> {
  return request<RepairApplyResult>('/library/repairs/apply', {
    method: 'POST',
    body: req,
    signal: options.signal,
  })
}
