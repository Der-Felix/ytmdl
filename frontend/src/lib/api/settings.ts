/** Server settings, provider status and health. */

import { request, requestList } from '@/lib/api/client'
import type { Health, ProviderInfo, Settings, SettingsUpdate } from '@/types/api'

/** GET /settings */
export function getSettings(signal?: AbortSignal): Promise<Settings> {
  return request<Settings>('/settings', { signal })
}

/**
 * PUT /settings
 *
 * The backend rejects an update that changes nothing, so callers must only
 * send fields whose value actually differs.
 */
export function updateSettings(
  update: SettingsUpdate,
  signal?: AbortSignal,
): Promise<Settings> {
  return request<Settings>('/settings', {
    method: 'PUT',
    body: update,
    signal,
  })
}

/**
 * GET /health
 *
 * Without a scope the answer includes the yt-dlp, ffmpeg and ffprobe probes;
 * scope=essential is the container's fast check and covers only the
 * application and the database.
 */
export function getHealth(
  options: { essential?: boolean; signal?: AbortSignal } = {},
): Promise<Health> {
  return request<Health>('/health', {
    query: { scope: options.essential ? 'essential' : undefined },
    signal: options.signal,
  })
}

/** GET /providers */
export async function listProviders(
  signal?: AbortSignal,
): Promise<ProviderInfo[]> {
  const result = await requestList<ProviderInfo>('/providers', { signal })
  return result.items
}

/**
 * The metadata provider the backend actually resolves requests with.
 *
 * This is read from /providers rather than from settings.default_metadata_provider:
 * that field reports the configured name even when the provider was never
 * registered — with no Spotify credentials it says "spotify" while every
 * request is in fact served by YouTube Music. The registry is the truth.
 */
export function effectiveMetadataProvider(
  providers: ProviderInfo[],
): ProviderInfo | undefined {
  return providers.find((p) => p.kind === 'metadata' && p.default)
}

/** The media provider requests actually fall back to. */
export function effectiveMediaProvider(
  providers: ProviderInfo[],
): ProviderInfo | undefined {
  return providers.find((p) => p.kind === 'media' && p.default)
}

/** True when a provider of that name is registered and usable. */
export function hasProvider(
  providers: ProviderInfo[],
  name: string,
  kind: 'metadata' | 'media' = 'metadata',
): boolean {
  return providers.some(
    (p) => p.name === name && p.kind === kind && p.available,
  )
}
