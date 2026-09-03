/** Artist subscriptions: watching an artist and syncing their discography. */

import { request, requestList, requestVoid } from '@/lib/api/client'
import type {
  ImportPreview,
  ImportResult,
  SubscribeRequest,
  Subscription,
  SubscriptionDetail,
  SubscriptionExport,
  SubscriptionUpdate,
  SyncStatus,
} from '@/types/api'

/** GET /subscriptions */
export async function listSubscriptions(
  options: {
    provider?: string
    artistSourceId?: string
    limit?: number
    signal?: AbortSignal
  } = {},
): Promise<Subscription[]> {
  const result = await requestList<Subscription>('/subscriptions', {
    query: {
      provider: options.provider,
      artist_source_id: options.artistSourceId,
      limit: options.limit,
    },
    signal: options.signal,
  })
  return result.items
}

/**
 * The subscription of one artist, or null when the artist is not watched.
 *
 * This is what the artist page asks. It goes through the list filter rather
 * than a lookup endpoint of its own, because "not subscribed" is a normal
 * answer and must not arrive as a 404 the caller has to catch.
 */
export async function findSubscription(
  provider: string,
  artistSourceId: string,
  signal?: AbortSignal,
): Promise<Subscription | null> {
  const list = await listSubscriptions({ provider, artistSourceId, signal })
  return list[0] ?? null
}

/**
 * POST /subscriptions
 *
 * Subscribing to an artist that is already watched returns the existing
 * subscription, so a double click is harmless.
 */
export function subscribe(
  body: SubscribeRequest,
  signal?: AbortSignal,
): Promise<Subscription> {
  return request<Subscription>('/subscriptions', { method: 'POST', body, signal })
}

/** GET /subscriptions/{id} — the subscription plus the last report, if any. */
export function getSubscription(
  id: string,
  signal?: AbortSignal,
): Promise<SubscriptionDetail> {
  return request<SubscriptionDetail>(`/subscriptions/${encodeURIComponent(id)}`, {
    signal,
  })
}

/** PATCH /subscriptions/{id} — an omitted field is left untouched. */
export function updateSubscription(
  id: string,
  body: SubscriptionUpdate,
  signal?: AbortSignal,
): Promise<Subscription> {
  return request<Subscription>(`/subscriptions/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body,
    signal,
  })
}

/**
 * DELETE /subscriptions/{id}
 *
 * Only the subscription goes. Everything that was already downloaded stays in
 * the library — a subscription says what to watch, not what to keep.
 */
export function unsubscribe(id: string, signal?: AbortSignal): Promise<void> {
  // The backend answers 204 with no body, so this must not go through
  // request(), which requires a "data" envelope.
  return requestVoid(`/subscriptions/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    signal,
  })
}

/**
 * POST /subscriptions/{id}/sync
 *
 * The backend answers 202 and walks the discography in the background, so the
 * answer means "started", never "finished". Progress arrives on the event
 * stream; the report is read back with getSubscription.
 */
export function syncSubscription(
  id: string,
  signal?: AbortSignal,
): Promise<Subscription> {
  return request<Subscription>(`/subscriptions/${encodeURIComponent(id)}/sync`, {
    method: 'POST',
    signal,
  })
}

/** GET /subscriptions/export — download all subscriptions in portable JSON format. */
export function getSubscriptionExport(signal?: AbortSignal): Promise<SubscriptionExport> {
  return request<SubscriptionExport>('/subscriptions/export', { signal })
}

/** Triggers a browser download of any JSON payload. */
export function downloadJsonFile(data: unknown, filename: string): void {
  const jsonStr = JSON.stringify(data, null, 2)
  const blob = new Blob([jsonStr], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

/** Export all subscriptions or a selected subset to a downloadable JSON file. */
export async function exportSubscriptionsToFile(selectedSubs?: Subscription[]): Promise<void> {
  const dateStr = new Date().toISOString().slice(0, 10)
  if (selectedSubs && selectedSubs.length > 0) {
    const payload: SubscriptionExport = {
      format: 'ytmdl-subscriptions',
      version: 1,
      exported_at: new Date().toISOString(),
      subscriptions: selectedSubs.map((s) => ({
        artist_name: s.artist_name,
        provider: s.provider,
        artist_source_id: s.artist_source_id,
        artist_image_url: s.artist_image_url,
        enabled: s.enabled,
        auto_download: s.auto_download,
        release_filter: s.release_filter,
        download_priority: s.download_priority,
      })),
    }
    downloadJsonFile(payload, `ytmdl-subscriptions-selected-${dateStr}.json`)
    return
  }
  const data = await getSubscriptionExport()
  downloadJsonFile(data, `ytmdl-subscriptions-${dateStr}.json`)
}

/** POST /subscriptions/import/preview — analyze subscriptions file without DB changes. */
export function previewImportSubscriptions(
  body: SubscriptionExport,
  signal?: AbortSignal,
): Promise<ImportPreview> {
  return request<ImportPreview>('/subscriptions/import/preview', {
    method: 'POST',
    body,
    signal,
  })
}

/** POST /subscriptions/import/apply — apply imported subscriptions in a transaction. */
export function applyImportSubscriptions(
  body: SubscriptionExport,
  signal?: AbortSignal,
): Promise<ImportResult> {
  return request<ImportResult>('/subscriptions/import/apply', {
    method: 'POST',
    body,
    signal,
  })
}

/* ------------------------------------------------------------------ helpers */

/** The German label of a sync status. */
export const SYNC_STATUS_LABELS: Record<SyncStatus, string> = {
  pending: 'Noch nicht geprüft',
  success: 'Erfolgreich',
  partial: 'Teilweise',
  failed: 'Fehlgeschlagen',
}

/** The badge tone a sync status is shown with. */
export function syncStatusTone(
  status: SyncStatus,
): 'neutral' | 'success' | 'warning' | 'destructive' {
  switch (status) {
    case 'success':
      return 'success'
    case 'partial':
      return 'warning'
    case 'failed':
      return 'destructive'
    default:
      return 'neutral'
  }
}

/**
 * The one-line summary of a finished run.
 *
 * A run that found nothing says so: "0 neue Releases · 0 neue Tracks" reads as
 * a failure at a glance, while "Keine Neuigkeiten" is what actually happened.
 */
export function summarizeSync(result: {
  new_releases: number
  new_tracks: number
  queued_tracks: number
}): string {
  const parts: string[] = []
  if (result.new_releases > 0) {
    parts.push(
      result.new_releases === 1
        ? '1 neues Release'
        : `${result.new_releases} neue Releases`,
    )
  }
  if (result.new_tracks > 0) {
    parts.push(
      result.new_tracks === 1 ? '1 neuer Track' : `${result.new_tracks} neue Tracks`,
    )
  }
  if (parts.length === 0) return 'Keine Neuigkeiten'

  if (result.queued_tracks > 0) {
    parts.push(
      result.queued_tracks === 1
        ? '1 Track in der Warteschlange'
        : `${result.queued_tracks} Tracks in der Warteschlange`,
    )
  }
  return parts.join(' · ')
}

/** Whether a subscription is due for its next scheduled run. */
export function isDue(subscription: Subscription, now: Date = new Date()): boolean {
  if (!subscription.enabled) return false
  const next = new Date(subscription.next_sync_at)
  if (Number.isNaN(next.getTime())) return false
  return next.getTime() <= now.getTime()
}
