import type { LibraryTrack } from '@/types/api'
import type { PlaylistDetail } from '@/types/playlist'
import { libraryArtwork } from '@/lib/artwork'
const DB_NAME = 'ytmdl.offline.v1'
const MAX_AUDIO = 64 * 1024 * 1024,
  MAX_PLAYLIST = 512 * 1024 * 1024
export interface OfflinePlaylist {
  key: string
  owner: string
  owner_name: string
  id: string
  name: string
  saved_at: string
  bytes: number
  tracks: LibraryTrack[]
}
interface OfflineAudio {
  key: string
  owner: string
  batch: string
  created_at: number
  audio: Blob
  cover?: Blob
}
let opening: Promise<IDBDatabase> | null = null
const urls = new Map<string, Promise<string>>()
function open(): Promise<IDBDatabase> {
  if (!globalThis.indexedDB)
    return Promise.reject(
      new Error('Dieser Browser unterstützt den lokalen Musikspeicher nicht.'),
    )
  if (!opening)
    opening = new Promise((resolve, reject) => {
      const req = indexedDB.open(DB_NAME, 1)
      req.onupgradeneeded = () => {
        req.result.createObjectStore('playlists', { keyPath: 'key' })
        req.result.createObjectStore('audio', { keyPath: 'key' })
      }
      req.onsuccess = () => {
        const db = req.result
        db.onversionchange = () => {
          db.close()
          opening = null
        }
        resolve(db)
      }
      req.onerror = () => {
        opening = null
        reject(new Error('Lokaler Musikspeicher konnte nicht geöffnet werden.'))
      }
      req.onblocked = () => {
        opening = null
        reject(
          new Error('Bitte andere YTMDL-Tabs schließen und erneut versuchen.'),
        )
      }
    })
  return opening
}
function completion(tx: IDBTransaction): Promise<void> {
  const result = new Promise<void>((resolve, reject) => {
    tx.oncomplete = () => resolve()
    tx.onerror = () =>
      reject(
        new Error(
          tx.error?.name === 'QuotaExceededError'
            ? 'Der Browser hat zu wenig freien Speicher. Vorhandene Offline-Kopien bleiben erhalten.'
            : 'Lokaler Musikspeicher konnte nicht geschrieben werden.',
        ),
      )
    tx.onabort = () =>
      reject(new Error('Lokaler Speichervorgang wurde abgebrochen.'))
  })
  void result.catch(() => {})
  return result
}
function read<T>(req: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result)
    req.onerror = () =>
      reject(new Error('Lokale Musik konnte nicht gelesen werden.'))
  })
}
export async function listOfflinePlaylists(
  owner?: string,
): Promise<OfflinePlaylist[]> {
  const db = await open()
  const tx = db.transaction('playlists', 'readonly')
  const result = await read<OfflinePlaylist[]>(
    tx.objectStore('playlists').getAll(),
  )
  return result
    .filter((p) => !owner || p.owner === owner)
    .sort((a, b) => b.saved_at.localeCompare(a.saved_at))
}
export async function boundedBlob(
  response: Response,
  limit: number,
  signal?: AbortSignal,
): Promise<Blob> {
  if (!response.ok)
    throw new Error(
      response.status === 401
        ? 'Bitte erneut anmelden, bevor du die Playlist speicherst.'
        : 'Eine Audiodatei ist nicht verfügbar. Die bisherige Offline-Kopie bleibt erhalten.',
    )
  if (Number(response.headers.get('content-length')) > limit) {
    void response.body?.cancel()
    throw new Error('Eine Datei überschreitet das Offline-Größenlimit.')
  }
  if (!response.body)
    throw new Error('Die Datei konnte nicht vollständig geladen werden.')
  const reader = response.body.getReader(),
    parts: Uint8Array<ArrayBuffer>[] = []
  let size = 0
  const abort = () => {
    void reader.cancel()
  }
  signal?.addEventListener('abort', abort, { once: true })
  try {
    for (;;) {
      signal?.throwIfAborted()
      const next = await reader.read()
      signal?.throwIfAborted()
      if (next.done) break
      size += next.value.byteLength
      if (size > limit)
        throw new Error('Eine Datei überschreitet das Offline-Größenlimit.')
      parts.push(next.value as Uint8Array<ArrayBuffer>)
    }
    if (!size) throw new Error('Die Datei ist leer.')
    return new Blob(parts, {
      type:
        response.headers.get('content-type')?.split(';')[0] ||
        'application/octet-stream',
    })
  } finally {
    signal?.removeEventListener('abort', abort)
    void reader.cancel()
  }
}
export async function saveOfflinePlaylist(
  owner: string,
  ownerName: string,
  playlist: PlaylistDetail,
  signal: AbortSignal,
  onProgress: (done: number, total: number) => void,
): Promise<OfflinePlaylist> {
  if (!playlist.tracks.length || playlist.tracks.length > 200)
    throw new Error('Offline-Kopien unterstützen 1–200 Titel pro Playlist.')
  signal = AbortSignal.any([signal, AbortSignal.timeout(60 * 60 * 1000)])
  await pruneOfflineStaging()
  const db = await open(),
    // getRandomValues also works on HTTP IP origins; randomUUID does not.
    batch = Array.from(crypto.getRandomValues(new Uint8Array(16)), (n) =>
      n.toString(16).padStart(2, '0'),
    ).join(''),
    keys: string[] = []
  let bytes = 0,
    committed = false
  const tracks: LibraryTrack[] = []
  try {
    for (let i = 0; i < playlist.tracks.length; i++) {
      signal.throwIfAborted()
      const track = playlist.tracks[i]!
      const response = await fetch(
        `/api/v1/library/tracks/${encodeURIComponent(track.id)}/stream`,
        { signal, credentials: 'same-origin', cache: 'no-store' },
      )
      const mime = response.headers.get('content-type')?.split(';')[0] || ''
      if (
        response.ok &&
        !mime.startsWith('audio/') &&
        !['application/ogg', 'application/octet-stream'].includes(mime)
      ) {
        void response.body?.cancel()
        throw new Error('Der Server hat keine Audiodatei geliefert.')
      }
      const audio = await boundedBlob(
        response,
        Math.min(MAX_AUDIO, MAX_PLAYLIST - bytes),
        signal,
      )
      bytes += audio.size
      const estimate = await navigator.storage?.estimate?.()
      if (
        estimate?.quota !== undefined &&
        estimate?.usage !== undefined &&
        estimate.quota - estimate.usage < audio.size + 1024 * 1024
      )
        throw new Error(
          'Der Browser hat nicht genügend freien Speicher. Vorhandene Offline-Kopien bleiben erhalten.',
        )
      let cover: Blob | undefined
      try {
        const image = await fetch(libraryArtwork('tracks', track.id), {
          signal,
          credentials: 'same-origin',
          cache: 'no-store',
        })
        if (image.ok && image.headers.get('content-type')?.startsWith('image/'))
          cover = await boundedBlob(image, 2 * 1024 * 1024, signal)
        else void image.body?.cancel()
      } catch {
        signal.throwIfAborted()
      }
      bytes += cover?.size || 0
      if (bytes > MAX_PLAYLIST)
        throw new Error(
          'Diese Playlist überschreitet 512 MiB. Bitte eine kleinere Auswahl speichern.',
        )
      const key = batch + ':' + i
      keys.push(key)
      const tx = db.transaction('audio', 'readwrite'),
        done = completion(tx)
      tx.objectStore('audio').put({
        key,
        owner,
        batch,
        created_at: Date.now(),
        audio,
        cover,
      } satisfies OfflineAudio)
      await done
      // Store display metadata, not credentials, private paths or provider URLs.
      const {
        file_path: _,
        source_url: __,
        cover_url: ___,
        ...metadata
      } = track
      tracks.push({
        ...metadata,
        source_url: '',
        cover_url: '',
        offline_blob_key: key,
      })
      onProgress(i + 1, playlist.tracks.length)
    }
    signal.throwIfAborted()
    const value: OfflinePlaylist = {
      key: JSON.stringify([owner, playlist.id]),
      owner,
      owner_name: ownerName,
      id: playlist.id,
      name: playlist.name,
      saved_at: new Date().toISOString(),
      bytes,
      tracks,
    }
    const tx = db.transaction(['audio', 'playlists'], 'readwrite'),
      done = completion(tx)
    // Keep explicit cancellation effective until the atomic publish completes.
    const abortCommit = () => {
      try {
        tx.abort()
      } catch {
        /* Already committed. */
      }
    }
    signal.addEventListener('abort', abortCommit, { once: true })
    try {
      signal.throwIfAborted()
      const previous = await read<OfflinePlaylist | undefined>(
        tx.objectStore('playlists').get(value.key),
      )
      const present = await Promise.all(
        keys.map((key) => read(tx.objectStore('audio').getKey(key))),
      )
      signal.throwIfAborted()
      if (present.some((key) => !key)) {
        tx.abort()
        await done
        throw new Error(
          'Eine vorbereitete Datei fehlt. Die bisherige Offline-Kopie bleibt erhalten.',
        )
      }

      tx.objectStore('playlists').put(value)
      for (const old of previous?.tracks || [])
        if (old.offline_blob_key)
          tx.objectStore('audio').delete(old.offline_blob_key)
      await done
      committed = true
      return value
    } finally {
      signal.removeEventListener('abort', abortCommit)
    }
  } catch (e) {
    if (!committed && keys.length) {
      try {
        const tx = db.transaction('audio', 'readwrite'),
          done = completion(tx)
        for (const key of keys) tx.objectStore('audio').delete(key)
        await done
      } catch {
        /* Keep the original error and original playlist; cleanup can be retried. */
      }
    }
    throw e
  }
}
export async function deleteOfflinePlaylist(key: string): Promise<void> {
  const db = await open(),
    tx = db.transaction(['audio', 'playlists'], 'readwrite'),
    done = completion(tx)
  const previous = await read<OfflinePlaylist | undefined>(
    tx.objectStore('playlists').get(key),
  )
  for (const track of previous?.tracks || [])
    if (track.offline_blob_key)
      tx.objectStore('audio').delete(track.offline_blob_key)
  tx.objectStore('playlists').delete(key)
  await done
}
export async function offlineAudioURL(key: string): Promise<string> {
  if (!urls.has(key))
    urls.set(
      key,
      (async () => {
        const db = await open()
        const record = await read<OfflineAudio | undefined>(
          db.transaction('audio', 'readonly').objectStore('audio').get(key),
        )
        if (!record?.audio)
          throw new Error(
            'Diese Offline-Kopie ist nicht mehr vorhanden. Bitte die Playlist erneut speichern.',
          )
        return URL.createObjectURL(record.audio)
      })().catch((e) => {
        urls.delete(key)
        throw e
      }),
    )
  return urls.get(key)!
}
export async function offlineCoverURL(
  key: string,
): Promise<string | undefined> {
  const db = await open(),
    record = await read<OfflineAudio | undefined>(
      db.transaction('audio', 'readonly').objectStore('audio').get(key),
    )
  return record?.cover ? URL.createObjectURL(record.cover) : undefined
}
export function releaseOfflineAudioURLs() {
  for (const value of urls.values())
    void value.then(
      (url) => URL.revokeObjectURL(url),
      () => {},
    )
  urls.clear()
}
export async function prepareOfflineShell(): Promise<boolean> {
  if (!window.isSecureContext || !('serviceWorker' in navigator)) return false
  await navigator.serviceWorker.register('/offline-sw.js', {
    scope: '/',
    updateViaCache: 'none',
  })
  let timer: ReturnType<typeof setTimeout> | undefined
  try {
    await Promise.race([
      navigator.serviceWorker.ready,
      new Promise((_, reject) => {
        timer = setTimeout(
          () =>
            reject(
              new Error(
                'App-Hülle konnte noch nicht offline vorbereitet werden. Musik ist gespeichert; bitte mit Serververbindung erneut versuchen.',
              ),
            ),
          20000,
        )
      }),
    ])
  } finally {
    clearTimeout(timer)
  }
  return true
}

export function forgetOfflineAudioURLs(keys: ReadonlySet<string>) {
  for (const key of keys) {
    const value = urls.get(key)
    if (value)
      void value.then(
        (url) => URL.revokeObjectURL(url),
        () => {},
      )
    urls.delete(key)
  }
}
export async function trimOfflineAudioURLs(protectedURLs: string[]) {
  if (urls.size <= 8) return
  for (const [key, value] of urls) {
    if (urls.size <= 8) break
    try {
      const url = await value
      if (!protectedURLs.includes(url)) {
        URL.revokeObjectURL(url)
        urls.delete(key)
      }
    } catch {
      urls.delete(key)
    }
  }
}
// Only abandoned, unreferenced staging from over a day ago is reclaimed.
// Completed copies are retained until the user explicitly removes them.
export async function pruneOfflineStaging(): Promise<void> {
  const db = await open()
  const tx = db.transaction(['audio', 'playlists'], 'readwrite'),
    done = completion(tx)
  const playlists = await read<OfflinePlaylist[]>(
      tx.objectStore('playlists').getAll(),
    ),
    referenced = new Set(
      playlists.flatMap((p) => p.tracks.map((t) => t.offline_blob_key)),
    )
  await new Promise<void>((resolve, reject) => {
    const cursor = tx.objectStore('audio').openCursor()
    cursor.onerror = () =>
      reject(new Error('Lokaler Speicher konnte nicht geprüft werden.'))
    cursor.onsuccess = () => {
      const item = cursor.result
      if (!item) {
        resolve()
        return
      }
      const value = item.value as OfflineAudio
      if (
        value.created_at &&
        value.created_at < Date.now() - 24 * 60 * 60 * 1000 &&
        !referenced.has(value.key)
      )
        item.delete()
      item.continue()
    }
  })
  await done
}
