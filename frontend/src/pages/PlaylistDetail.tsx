import { useCallback, useMemo, useState } from 'react'
import {
  ArrowLeft,
  Heart,
  Loader2,
  Pencil,
  Pause,
  Play,
  Shuffle,
  Trash2,
} from 'lucide-react'

import { Cover } from '@/components/music/Cover'
import { PlaylistArtwork } from '@/components/music/PlaylistArtwork'
import { PlaylistTrackMenu } from '@/components/music/PlaylistTrackMenu'
import { libraryArtwork } from '@/lib/artwork'
import { Badge } from '@/components/ui/badge'
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
import { useOptionalFavorites } from '@/hooks/useFavorites'
import { usePlayerActions, usePlayerState } from '@/hooks/usePlayer'
import {
  deletePlaylist,
  getPlaylist,
  removePlaylistTrack,
  reorderPlaylistTracks,
  updatePlaylist,
} from '@/lib/api/playlists'
import { Link, useNavigate, paths } from '@/lib/router'
import { formatDuration, joinArtists } from '@/lib/utils/format'
import { SmartRuleEditor } from '@/components/music/SmartRuleEditor'
import { OfflineSavePanel } from '@/components/music/OfflineSavePanel'
import { setPlaylistRules } from '@/lib/api/libraryTools'
import type { SmartRules } from '@/types/playlist'
import type { PlaylistTrack } from '@/types/playlist'

interface PlaylistDetailProps {
  id: string
}

export function PlaylistDetail({ id }: PlaylistDetailProps) {
  const navigate = useNavigate()
  const { currentTrack, status } = usePlayerState()
  const { playTrack, playAlbum, togglePlayPause, playNext, addToQueue } = usePlayerActions()
  const favorites = useOptionalFavorites()

  const { state, reload, setData } = useAsync((signal) => getPlaylist(id, signal), [id])

  // Edit modal
  const [editDialogOpen, setEditDialogOpen] = useState(false)
  const [editName, setEditName] = useState('')
  const [editDescription, setEditDescription] = useState('')
  const [editRules, setEditRules] = useState<SmartRules | null>(null)
  const [updating, setUpdating] = useState(false)
  const [updateError, setUpdateError] = useState<string | null>(null)

  // Delete modal
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  // Track removal/reordering feedback
  const [reordering, setReordering] = useState(false)
  const [actionTrackId, setActionTrackId] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [removing, setRemoving] = useState<PlaylistTrack | null>(null)

  const playlist = state.status === 'success' ? state.data : null

  // Calculate total duration
  const totalDurationMs = useMemo(() => {
    if (!playlist?.tracks) return 0
    return playlist.tracks.reduce((acc, pt) => acc + (pt.duration_ms || 0), 0)
  }, [playlist])

  // Play entire playlist
  const handlePlayAll = useCallback(() => {
    if (!playlist?.tracks || playlist.tracks.length === 0) return
    playAlbum(playlist.tracks)
  }, [playlist, playAlbum])

  // Play shuffled
  const handlePlayShuffled = useCallback(() => {
    if (!playlist?.tracks || playlist.tracks.length === 0) return
    const shuffled = [...playlist.tracks].sort(() => Math.random() - 0.5)
    playAlbum(shuffled)
  }, [playlist, playAlbum])

  // Play specific track
  const handlePlayTrack = useCallback(
    (track: PlaylistTrack, index: number) => {
      if (!playlist?.tracks) return
      if (currentTrack?.id === track.id) {
        togglePlayPause()
      } else {
        playTrack(track, playlist.tracks, index)
      }
    },
    [currentTrack?.id, playlist, playTrack, togglePlayPause],
  )

  // Open edit dialog
  const openEditModal = () => {
    if (!playlist) return
    setEditName(playlist.name)
    setEditDescription(playlist.description || '')
    setEditRules(playlist.smart_rules || null)
    setUpdateError(null)
    setEditDialogOpen(true)
  }

  // Submit edit
  const handleUpdate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!editName.trim()) return

    try {
      setUpdating(true)
      setUpdateError(null)
      const updated = await updatePlaylist(id, {
        name: editName.trim(),
        description: editDescription.trim(),
      })
      setData((prev) => ({
        ...prev,
        name: updated.name,
        description: updated.description,
        updated_at: updated.updated_at,
      }))
      await setPlaylistRules(id, editRules)
      void reload()
      setEditDialogOpen(false)
    } catch (err: unknown) {
      setUpdateError(err instanceof Error ? err.message : 'Fehler beim Aktualisieren der Playlist')
    } finally {
      setUpdating(false)
    }
  }

  // Submit delete
  const handleDelete = async () => {
    try {
      setDeleting(true)
      setDeleteError(null)
      await deletePlaylist(id)
      setDeleteDialogOpen(false)
      navigate(paths.playlists())
    } catch (err: unknown) {
      setDeleteError(err instanceof Error ? err.message : 'Fehler beim Löschen der Playlist')
    } finally {
      setDeleting(false)
    }
  }

  // Remove track
  const handleRemoveTrack = async (trackId: string) => {
    if (!playlist || actionTrackId || reordering) return
    try {
      setActionError(null)
      setActionTrackId(trackId)
      // Optimistic update
      const prevTracks = playlist.tracks
      const filtered = prevTracks
        .filter((pt) => pt.id !== trackId)
        .map((pt, idx) => ({ ...pt, position: idx + 1 }))

      setData((prev) => ({
        ...prev,
        track_count: filtered.length,
        tracks: filtered,
      }))

      await removePlaylistTrack(id, trackId)
    } catch (error) {
      setActionError(error instanceof Error ? error.message : 'Titel konnte nicht entfernt werden.')
      // Rollback on error
      void reload()
    } finally {
      setActionTrackId(null)
    }
  }

  // Move track position up/down
  const handleMoveTrack = async (index: number, direction: 'up' | 'down') => {
    if (!playlist || reordering || actionTrackId) return
    setActionError(null)
    const targetIndex = direction === 'up' ? index - 1 : index + 1
    if (targetIndex < 0 || targetIndex >= playlist.tracks.length) return

    const tracksCopy = [...playlist.tracks]
    const [moved] = tracksCopy.splice(index, 1)
    if (!moved) return
    tracksCopy.splice(targetIndex, 0, moved)

    const reorderedTracks: PlaylistTrack[] = tracksCopy.map((pt, idx) => ({
      ...pt,
      position: idx + 1,
    }))

    const newTrackIds = reorderedTracks.map((pt) => pt.id)

    // Optimistic update
    setData((prev) => ({
      ...prev,
      tracks: reorderedTracks,
    }))

    try {
      setReordering(true)
      await reorderPlaylistTracks(id, newTrackIds)
    } catch (error) {
      setActionError(error instanceof Error ? error.message : 'Reihenfolge konnte nicht gespeichert werden.')
      // Rollback
      void reload()
    } finally {
      setReordering(false)
    }
  }

  if (state.status === 'loading') {
    return (
      <div className="space-y-6 pb-24">
        <div className="flex items-center gap-4">
          <Link
            href={paths.playlists()}
            className="flex items-center gap-1.5 text-xs text-neutral-400 hover:text-white"
          >
            <ArrowLeft className="size-4" />
            Zurück zu Playlists
          </Link>
        </div>
        <ListSkeleton rows={5} />
      </div>
    )
  }

  if (state.status === 'error' || !playlist) {
    return (
      <div className="space-y-6 pb-24">
        <Link
          href={paths.playlists()}
          className="inline-flex items-center gap-1.5 text-xs text-neutral-400 hover:text-white"
        >
          <ArrowLeft className="size-4" />
          Zurück zu Playlists
        </Link>
        <ErrorState
          error={state.status === 'error' ? state.error : new Error('Playlist nicht gefunden')}
          onRetry={reload}
        />
      </div>
    )
  }

  const tracks = playlist.tracks || []
  const visible = tracks.map((track, index) => ({ track, index })).filter(({ track }) =>
    `${track.title} ${joinArtists(track.artists || [])} ${track.album || ''}`.toLocaleLowerCase('de').includes(query.trim().toLocaleLowerCase('de')))

  return (
    <div className="space-y-6 pb-28">
      {/* Navigation Breadcrumb */}
      <div className="flex items-center justify-between">
        <Link
          href={paths.playlists()}
          className="inline-flex items-center gap-1.5 text-xs text-neutral-400 hover:text-white transition-colors"
        >
          <ArrowLeft className="size-4" />
          Zurück zu Playlists
        </Link>
      </div>

      {/* Playlist Hero Header */}
      <div className="flex flex-col md:flex-row md:items-end gap-6 p-6 rounded-3xl border border-white/5 bg-gradient-to-b from-white/[0.04] to-white/[0.01]">
        <PlaylistArtwork tracks={tracks} name={playlist.name} className="size-44 sm:size-56 shrink-0 shadow-xl" />

        <div className="flex-1 min-w-0 space-y-2">
          <Badge
            variant="outline"
            className="text-[11px] uppercase tracking-wider text-primary border-primary/30"
          >
            {playlist.smart_rules ? 'Intelligente Playlist' : 'Playlist'}
          </Badge>
          <h1 className="text-3xl sm:text-5xl font-bold tracking-tight text-white break-words">
            {playlist.name}
          </h1>
          {playlist.description && (
            <p className="text-sm text-neutral-300 leading-relaxed max-w-2xl">
              {playlist.description}
            </p>
          )}

          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 pt-1 text-xs text-neutral-400 font-mono">
            <span>
              {playlist.track_count} {playlist.track_count === 1 ? 'Titel' : 'Titel'}
            </span>
            <span>•</span>
            <span>{formatDuration(totalDurationMs)}</span>
          </div>

          {/* Action Buttons */}
          <div className="flex flex-wrap items-center gap-2.5 pt-3">
            <Button
              onClick={handlePlayAll}
              disabled={tracks.length === 0}
              className="gap-2 shadow-lg shadow-primary/20"
            >
              <Play className="size-4 fill-current" />
              Playlist abspielen
            </Button>
            <Button
              variant="outline"
              onClick={handlePlayShuffled}
              disabled={tracks.length === 0}
              className="gap-2"
            >
              <Shuffle className="size-4" />
              Zufall
            </Button>
            <Button
              variant="ghost"
              onClick={openEditModal}
              className="rounded-full text-neutral-400 hover:text-white"
              title="Playlist bearbeiten"
            >
              <Pencil className="size-4" /> Bearbeiten
            </Button>
            <Button
              variant="ghost"
              size="icon"
              onClick={() => {
                setDeleteError(null)
                setDeleteDialogOpen(true)
              }}
              className="rounded-full text-neutral-400 hover:text-destructive"
              title="Playlist löschen"
              aria-label="Playlist löschen"
            >
              <Trash2 className="size-4" />
            </Button>
          </div>
        </div>
      </div>

      <details className="rounded-2xl border border-white/10 bg-white/[0.02]">
        <summary className="cursor-pointer rounded-2xl px-5 py-4 text-sm font-medium hover:bg-white/5 focus-visible:outline-2 focus-visible:outline-ring">Offline mitnehmen · Download-Optionen</summary>
        <div className="p-3 pt-0"><OfflineSavePanel playlist={playlist} /></div>
      </details>

      <section aria-label="Titel dieser Playlist" className="rounded-2xl border border-white/10 bg-white/[0.02]">
        <div className="flex flex-col gap-3 border-b border-white/10 p-4 sm:flex-row sm:items-center sm:justify-between">
          <div><h2 className="text-lg font-semibold">Titel</h2><p className="text-sm text-muted-foreground">{visible.length} von {tracks.length} · Reihenfolge der Playlist</p></div>
          <Input aria-label="In dieser Playlist suchen" placeholder="Titel, Künstler oder Album suchen …" value={query} onChange={(event) => setQuery(event.target.value)} className="sm:max-w-sm" />
        </div>
        {actionError && <p role="alert" className="px-5 pt-4 text-sm text-destructive">{actionError}</p>}
        {tracks.length === 0 ? <EmptyState title="Noch keine Titel" description="Füge Musik aus der Bibliothek über „Zur Playlist hinzufügen“ hinzu." action={<Link href={paths.library()} className="text-primary hover:underline">Bibliothek öffnen</Link>} /> :
          visible.length === 0 ? <div className="p-10 text-center"><p>Keine passenden Titel.</p><Button variant="ghost" onClick={() => setQuery('')}>Suche zurücksetzen</Button></div> :
          <ol className="divide-y divide-white/5">
            {visible.map(({ track: pt, index }) => {
              const isCurrent = currentTrack?.id === pt.id
              const isPlaying = isCurrent && status === 'playing'
              const isFav = favorites?.isFavorite(pt.id)
              return <li key={pt.id} className={`flex items-center gap-3 px-3 py-3 sm:gap-4 sm:px-5 ${isCurrent ? 'bg-primary/8' : 'hover:bg-white/[0.035]'} transition-colors`}>
                <Button variant="ghost" size="icon" className={`size-11 shrink-0 rounded-full ${isCurrent ? 'bg-primary/15 text-primary' : ''}`} aria-label={`${pt.title} ${isPlaying ? 'pausieren' : 'abspielen'}`} onClick={() => handlePlayTrack(pt, index)}>
                  {isPlaying ? <Pause className="size-4 fill-current" /> : <Play className="size-4 fill-current" />}
                </Button>
                <Cover src={libraryArtwork('tracks', pt.id)} alt={pt.title} className="size-12 shrink-0 rounded-lg sm:size-14" />
                <button onClick={() => handlePlayTrack(pt, index)} aria-label={`${pt.title} auswählen`} className="min-w-0 flex-1 rounded-lg text-left focus-visible:outline-2 focus-visible:outline-ring">
                  <span className={`block truncate text-base font-medium ${isCurrent ? 'text-primary' : ''}`}>{pt.title}</span>
                  <span className="block truncate text-sm text-muted-foreground">{joinArtists(pt.artists || []) || pt.album_artist} <span className="hidden sm:inline">· {pt.album || 'Ohne Album'}</span></span>
                </button>
                {pt.codec && <Badge variant="outline" className="hidden uppercase text-xs lg:inline-flex">{pt.codec}</Badge>}
                <span className="hidden text-sm tabular-nums text-muted-foreground sm:block">{formatDuration(pt.duration_ms)}</span>
                {favorites && <Button variant="ghost" size="icon" className={`size-11 shrink-0 ${isFav ? 'text-primary' : 'text-muted-foreground'}`} aria-label={`${pt.title}: ${isFav ? 'Aus Favoriten entfernen' : 'Zu Favoriten hinzufügen'}`} onClick={() => { setActionError(null); void favorites.toggleFavorite(pt.id).catch((error: unknown) => setActionError(error instanceof Error ? error.message : 'Favorit konnte nicht gespeichert werden.')) }}><Heart className={`size-5 ${isFav ? 'fill-current' : ''}`} /></Button>}
                <PlaylistTrackMenu title={pt.title} smart={!!playlist.smart_rules} busy={reordering || !!actionTrackId}
                  moveUp={index > 0 ? () => void handleMoveTrack(index, 'up') : undefined}
                  moveDown={index < tracks.length - 1 ? () => void handleMoveTrack(index, 'down') : undefined}
                  next={() => playNext(pt)} enqueue={() => addToQueue(pt)} remove={() => setRemoving(pt)} />
              </li>
            })}
          </ol>}
      </section>
      <Dialog open={removing !== null} onOpenChange={(open) => { if (!open) setRemoving(null) }}>
        <DialogContent><DialogHeader><DialogTitle>Titel aus Playlist entfernen?</DialogTitle><DialogDescription>{removing?.title} bleibt in deiner Bibliothek. Nur der Eintrag in dieser Playlist wird entfernt.</DialogDescription></DialogHeader><DialogFooter>
          <Button variant="ghost" onClick={() => setRemoving(null)}>Abbrechen</Button><Button variant="destructive" onClick={() => { if (removing) void handleRemoveTrack(removing.id); setRemoving(null) }}>Aus Playlist entfernen</Button>
        </DialogFooter></DialogContent>
      </Dialog>

      {/* Edit Dialog */}
      <Dialog open={editDialogOpen} onOpenChange={setEditDialogOpen}>
        <DialogContent className="max-w-md">
          <form onSubmit={(e) => void handleUpdate(e)}>
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                <Pencil className="size-5 text-primary" />
                Playlist bearbeiten
              </DialogTitle>
              <DialogDescription>
                Passe den Namen und die Beschreibung deiner Playlist an.
              </DialogDescription>
            </DialogHeader>

            {updateError && (
              <div className="my-3 rounded-lg bg-destructive/10 border border-destructive/20 p-2.5 text-xs text-destructive">
                {updateError}
              </div>
            )}

            <div className="space-y-4 py-4 max-h-[65dvh] overflow-y-auto">
              <SmartRuleEditor value={editRules} onChange={setEditRules} />
              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-neutral-300">
                  Name <span className="text-destructive">*</span>
                </label>
                <Input
                  autoFocus
                  value={editName}
                  onChange={(e) => setEditName(e.target.value)}
                  maxLength={100}
                  required
                />
              </div>

              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-neutral-300">Beschreibung</label>
                <Input
                  value={editDescription}
                  onChange={(e) => setEditDescription(e.target.value)}
                  maxLength={500}
                />
              </div>
            </div>

            <DialogFooter>
              <Button
                type="button"
                variant="ghost"
                disabled={updating}
                onClick={() => setEditDialogOpen(false)}
              >
                Abbrechen
              </Button>
              <Button type="submit" disabled={updating || !editName.trim()}>
                {updating ? <Loader2 className="size-4 animate-spin mr-2" /> : null}
                Speichern
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Delete Confirmation Dialog */}
      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-destructive">
              <Trash2 className="size-5" />
              Playlist wirklich löschen?
            </DialogTitle>
            <DialogDescription>
              Möchtest du die Playlist &quot;{playlist.name}&quot; wirklich löschen? Die Titel in
              deiner Bibliothek bleiben davon unberührt.
            </DialogDescription>
          </DialogHeader>

          {deleteError && (
            <div className="my-3 rounded-lg bg-destructive/10 border border-destructive/20 p-2.5 text-xs text-destructive">
              {deleteError}
            </div>
          )}

          <DialogFooter className="gap-2">
            <Button
              type="button"
              variant="ghost"
              disabled={deleting}
              onClick={() => setDeleteDialogOpen(false)}
            >
              Abbrechen
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={deleting}
              onClick={() => void handleDelete()}
            >
              {deleting ? <Loader2 className="size-4 animate-spin mr-2" /> : null}
              Endgültig löschen
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
