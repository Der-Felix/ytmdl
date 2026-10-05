import { useCallback, useEffect, useMemo, useState } from 'react'
import { ListMusic, Loader2, Play, Plus } from 'lucide-react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { EmptyState, ErrorState, ListSkeleton } from '@/components/ui/state-view'
import { useAsync } from '@/hooks/useAsync'
import { usePlayerActions } from '@/hooks/usePlayer'
import { createPlaylist, getPlaylist, listPlaylists } from '@/lib/api/playlists'
import { Link, useNavigate, paths } from '@/lib/router'
import { PlaylistArtwork } from '@/components/music/PlaylistArtwork'
import { playlistPreview } from '@/lib/playlist-artwork'
import { Pagination } from '@/components/ui/pagination'
import type { LibraryTrack } from '@/types/api'
import { formatDuration, formatRelative } from '@/lib/utils/format'
import { SmartRuleEditor } from '@/components/music/SmartRuleEditor'
import type { SmartRules } from '@/types/playlist'
import type { Playlist } from '@/types/playlist'

export function Playlists() {
  const navigate = useNavigate()
  const { playAlbum } = usePlayerActions()
  const { state, reload, setData } = useAsync((signal) => listPlaylists(signal), [])

  const [createDialogOpen, setCreateDialogOpen] = useState(false)
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [rules, setRules] = useState<SmartRules | null>(null)
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [playingPlaylistId, setPlayingPlaylistId] = useState<string | null>(null)
  const [playError, setPlayError] = useState('')
  const [query, setQuery] = useState('')
  const [sort, setSort] = useState('recent')
  const [page, setPage] = useState(1)
  const [previews, setPreviews] = useState<Record<string, LibraryTrack[]>>({})
  const all = useMemo(() => state.status === 'success' ? state.data : [], [state])
  const filtered = useMemo(() => {
    const matches = all.filter((playlist) => `${playlist.name} ${playlist.description || ''}`.toLocaleLowerCase('de').includes(query.trim().toLocaleLowerCase('de')))
    return sort === 'name' ? matches.sort((a, b) => a.name.localeCompare(b.name, 'de')) : matches
  }, [all, query, sort])
  const visible = useMemo(() => filtered.slice((page - 1) * 12, page * 12), [filtered, page])
  // The legacy API has no preview field. Bound detail requests to the current
  // page and three concurrent transfers; retain only four covers per playlist.
  useEffect(() => {
    const controller = new AbortController()
    async function load() {
      for (let start = 0; start < visible.length; start += 3) {
        if (controller.signal.aborted) return
        await Promise.all(visible.slice(start, start + 3).map(async (playlist) => {
          if (playlist.track_count === 0) return
          try {
            const detail = await getPlaylist(playlist.id, controller.signal)
            if (!controller.signal.aborted) setPreviews((previous) => ({ ...previous, [playlist.id]: playlistPreview(detail.tracks || []) }))
          } catch { /* Broken previews retain their placeholder. */ }
        }))
      }
    }
    void load()
    return () => controller.abort()
  }, [visible])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim()) return

    try {
      setCreating(true)
      setCreateError(null)
      const created = await createPlaylist({
        name: name.trim(),
        description: description.trim(),
        smart_rules: rules,
      })
      setData((prev) => (prev ? [created, ...prev] : [created]))
      setCreateDialogOpen(false)
      setName('')
      setRules(null)
      setDescription('')
      navigate(paths.playlist(created.id))
    } catch (err: unknown) {
      setCreateError(err instanceof Error ? err.message : 'Fehler beim Erstellen der Playlist')
    } finally {
      setCreating(false)
    }
  }

  const handlePlayPlaylist = useCallback(
    async (e: React.MouseEvent, playlist: Playlist) => {
      e.stopPropagation()
      e.preventDefault()
      try {
        setPlayError('')
        setPlayingPlaylistId(playlist.id)
        const detail = await getPlaylist(playlist.id)
        if (detail.tracks && detail.tracks.length > 0) {
          playAlbum(detail.tracks)
        }
      } catch (error) {
        setPlayError(error instanceof Error ? error.message : 'Playlist konnte nicht geladen werden.')
      } finally {
        setPlayingPlaylistId(null)
      }
    },
    [playAlbum],
  )

  return (
    <div className="space-y-6 pb-24">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl sm:text-3xl font-bold tracking-tight text-white flex items-center gap-3">
            <ListMusic className="size-7 text-primary" />
            Playlists
          </h1>
          <p className="text-sm text-neutral-400 mt-1">Deine persönlichen Wiedergabelisten</p>
        </div>
        <Button
          onClick={() => {
            setName('')
            setRules(null)
            setDescription('')
            setCreateError(null)
            setCreateDialogOpen(true)
          }}
          className="shrink-0 gap-2"
        >
          <Plus className="size-4" />
          Neue Playlist
        </Button>
      </div>

      {/* Main Content */}
      {state.status === 'loading' && <ListSkeleton rows={4} />}

      {state.status === 'error' && <ErrorState error={state.error} onRetry={reload} />}

      {state.status === 'success' && state.data.length === 0 && (
        <EmptyState
          icon={<ListMusic />}
          title="Keine Playlists vorhanden"
          description="Erstelle deine erste Playlist, um deine Lieblingstitel individuell zu organisieren."
          action={
            <Button
              onClick={() => {
                setName('')
                setRules(null)
                setDescription('')
                setCreateError(null)
                setCreateDialogOpen(true)
              }}
              className="gap-2 mt-2"
            >
              <Plus className="size-4" />
              Playlist erstellen
            </Button>
          }
        />
      )}

      {state.status === 'success' && all.length > 0 && <>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
          <Input aria-label="Playlists durchsuchen" placeholder="Deine Playlists durchsuchen …" value={query} onChange={(event) => { setQuery(event.target.value); setPage(1) }} className="sm:max-w-md" />
          <select aria-label="Playlists sortieren" value={sort} onChange={(event) => { setSort(event.target.value); setPage(1) }} className="rounded-xl border border-border bg-background px-3 py-2 text-sm">
            <option value="recent">Zuletzt geändert</option><option value="name">Name A–Z</option>
          </select>
          <span className="text-sm text-muted-foreground sm:ml-auto">{filtered.length} Playlists</span>
        </div>
        {playError && <p role="alert" className="text-sm text-destructive">{playError}</p>}
        {!filtered.length && <EmptyState title="Keine passende Playlist" description="Versuche einen anderen Namen oder setze die Suche zurück." action={<Button variant="outline" onClick={() => setQuery('')}>Suche zurücksetzen</Button>} />}
        <div className="grid grid-cols-2 gap-4 md:grid-cols-3 xl:grid-cols-4 sm:gap-6">
          {visible.map((playlist) => <article key={playlist.id} className="min-w-0 rounded-2xl border border-white/10 bg-white/[0.025] p-3 transition-colors hover:bg-white/[0.045] sm:p-4">
            <Link href={paths.playlist(playlist.id)} className="block rounded-xl focus-visible:outline-2 focus-visible:outline-ring">
              <PlaylistArtwork name={playlist.name} tracks={previews[playlist.id] || []} className="mb-4 w-full" />
              <h2 className="truncate text-base font-semibold sm:text-lg">{playlist.name}</h2>
              <p className="mt-1 text-sm text-muted-foreground">{playlist.track_count} Titel · {formatDuration(playlist.duration_ms)}</p>
              {playlist.smart_rules && <span className="mt-2 inline-block text-xs text-primary">Intelligente Playlist</span>}
              {playlist.description && <p className="mt-2 line-clamp-2 text-sm text-muted-foreground">{playlist.description}</p>}
            </Link>
            <div className="mt-4 flex items-center justify-between gap-2 border-t border-white/5 pt-3">
              <span className="hidden text-xs text-muted-foreground sm:block">{formatRelative(playlist.updated_at)}</span>
              <Button variant="secondary" size="sm" disabled={!playlist.track_count || playingPlaylistId !== null} onClick={(event) => void handlePlayPlaylist(event, playlist)} aria-label={`${playlist.name} abspielen`} className="gap-2">
                {playingPlaylistId === playlist.id ? <Loader2 className="size-4 animate-spin" /> : <Play className="size-4 fill-current" />} Abspielen
              </Button>
            </div>
          </article>)}
        </div>
        <Pagination page={page} pageSize={12} total={filtered.length} onPageChange={setPage} />
      </>}

      {/* Create Dialog */}
      <Dialog open={createDialogOpen} onOpenChange={setCreateDialogOpen}>
        <DialogContent className="max-w-md">
          <form onSubmit={(e) => void handleCreate(e)}>
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                <ListMusic className="size-5 text-primary" />
                Neue Playlist erstellen
              </DialogTitle>
              <DialogDescription>
                Gib deiner neuen Playlist einen Namen und eine optionale Beschreibung.
              </DialogDescription>
            </DialogHeader>

            {createError && (
              <div className="my-3 rounded-lg bg-destructive/10 border border-destructive/20 p-2.5 text-xs text-destructive">
                {createError}
              </div>
            )}

            <div className="space-y-4 py-4 max-h-[65dvh] overflow-y-auto">
              <SmartRuleEditor value={rules} onChange={setRules} />
              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-neutral-300">
                  Name <span className="text-destructive">*</span>
                </label>
                <Input
                  autoFocus
                  placeholder="z.B. Rock Classics, Chillout..."
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  maxLength={100}
                  required
                />
              </div>

              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-neutral-300">
                  Beschreibung (optional)
                </label>
                <Input
                  placeholder="Kurze Notiz oder Genre..."
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  maxLength={500}
                />
              </div>
            </div>

            <DialogFooter>
              <Button
                type="button"
                variant="ghost"
                disabled={creating}
                onClick={() => setCreateDialogOpen(false)}
              >
                Abbrechen
              </Button>
              <Button type="submit" disabled={creating || !name.trim()}>
                {creating ? <Loader2 className="size-4 animate-spin mr-2" /> : null}
                Erstellen
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}
