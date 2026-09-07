import { useCallback, useMemo, useState } from 'react'
import {
  ArrowDown,
  ArrowLeft,
  ArrowUp,
  Heart,
  ListMusic,
  Loader2,
  Pause,
  Pencil,
  Play,
  Radio,
  Shuffle,
  Trash2,
} from 'lucide-react'

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
import type { PlaylistTrack } from '@/types/playlist'

interface PlaylistDetailProps {
  id: string
}

export function PlaylistDetail({ id }: PlaylistDetailProps) {
  const navigate = useNavigate()
  const { currentTrack, status } = usePlayerState()
  const { playTrack, playAlbum, togglePlayPause } = usePlayerActions()
  const favorites = useOptionalFavorites()

  const { state, reload, setData } = useAsync(
    (signal) => getPlaylist(id, signal),
    [id],
  )

  // Edit modal
  const [editDialogOpen, setEditDialogOpen] = useState(false)
  const [editName, setEditName] = useState('')
  const [editDescription, setEditDescription] = useState('')
  const [updating, setUpdating] = useState(false)
  const [updateError, setUpdateError] = useState<string | null>(null)

  // Delete modal
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  // Track removal/reordering feedback
  const [reordering, setReordering] = useState(false)
  const [actionTrackId, setActionTrackId] = useState<string | null>(null)

  const playlist = state.status === 'success' ? state.data : null

  // Calculate total duration
  const totalDurationMs = useMemo(() => {
    if (!playlist?.tracks) return 0
    return playlist.tracks.reduce((acc, pt) => acc + (pt.duration_ms || 0), 0)
  }, [playlist?.tracks])

  // Play entire playlist
  const handlePlayAll = useCallback(() => {
    if (!playlist?.tracks || playlist.tracks.length === 0) return
    playAlbum(playlist.tracks)
  }, [playlist?.tracks, playAlbum])

  // Play shuffled
  const handlePlayShuffled = useCallback(() => {
    if (!playlist?.tracks || playlist.tracks.length === 0) return
    const shuffled = [...playlist.tracks].sort(() => Math.random() - 0.5)
    playAlbum(shuffled)
  }, [playlist?.tracks, playAlbum])

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
    [currentTrack?.id, playlist?.tracks, playTrack, togglePlayPause],
  )

  // Open edit dialog
  const openEditModal = () => {
    if (!playlist) return
    setEditName(playlist.name)
    setEditDescription(playlist.description || '')
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
      setEditDialogOpen(false)
    } catch (err: unknown) {
      setUpdateError(
        err instanceof Error ? err.message : 'Fehler beim Aktualisieren der Playlist',
      )
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
      setDeleteError(
        err instanceof Error ? err.message : 'Fehler beim Löschen der Playlist',
      )
    } finally {
      setDeleting(false)
    }
  }

  // Remove track
  const handleRemoveTrack = async (trackId: string) => {
    if (!playlist) return
    try {
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
    } catch {
      // Rollback on error
      void reload()
    } finally {
      setActionTrackId(null)
    }
  }

  // Move track position up/down
  const handleMoveTrack = async (index: number, direction: 'up' | 'down') => {
    if (!playlist || reordering) return
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
    } catch {
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
        <div className="size-36 sm:size-44 rounded-2xl bg-gradient-to-br from-neutral-800 to-neutral-900 border border-white/10 flex items-center justify-center shrink-0 shadow-2xl">
          <ListMusic className="size-20 text-neutral-600" />
        </div>

        <div className="flex-1 min-w-0 space-y-2">
          <Badge variant="outline" className="text-[11px] uppercase tracking-wider text-primary border-primary/30">
            Playlist
          </Badge>
          <h1 className="text-2xl sm:text-4xl font-extrabold tracking-tight text-white break-words">
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
              size="icon"
              onClick={openEditModal}
              className="rounded-full text-neutral-400 hover:text-white"
              title="Playlist bearbeiten"
            >
              <Pencil className="size-4" />
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
            >
              <Trash2 className="size-4" />
            </Button>
          </div>
        </div>
      </div>

      {/* Track List */}
      {tracks.length === 0 ? (
        <EmptyState
          icon={<ListMusic />}
          title="Diese Playlist ist noch leer"
          description="Füge Titel aus der Bibliothek oder dem Player mit dem Ordner-Symbol hinzu."
          action={
            <Link href={paths.library()}>
              <Button variant="outline" className="gap-2 mt-2">
                Zur Bibliothek
              </Button>
            </Link>
          }
        />
      ) : (
        <div className="w-full overflow-x-auto rounded-2xl border border-white/5 bg-neutral-950/40">
          <table className="w-full text-left text-sm border-collapse min-w-[650px]">
            <thead>
              <tr className="border-b border-white/5 bg-white/[0.02] text-neutral-400">
                <th className="py-3 px-3 w-12 text-center">#</th>
                <th className="py-3 px-3">Titel</th>
                <th className="py-3 px-3">Künstler</th>
                <th className="py-3 px-3 hidden md:table-cell">Album</th>
                <th className="py-3 px-3 w-28 text-center">Format</th>
                <th className="py-3 px-3 w-20 text-right">Dauer</th>
                <th className="py-3 px-2 w-36 text-right"></th>
              </tr>
            </thead>
            <tbody className="divide-y divide-white/5">
              {tracks.map((pt, index) => {
                const isCurrent = currentTrack?.id === pt.id
                const isPlaying = isCurrent && status === 'playing'
                const artistName =
                  pt.artists?.length > 0
                    ? joinArtists(pt.artists)
                    : pt.album_artist || ''
                const isFav = favorites?.isFavorite(pt.id)

                return (
                  <tr
                    key={pt.id}
                    onClick={() => handlePlayTrack(pt, index)}
                    className={`transition-colors cursor-pointer group ${
                      isCurrent ? 'bg-white/[0.05]' : 'hover:bg-white/[0.02]'
                    }`}
                  >
                    {/* Position / Play Button */}
                    <td className="py-2.5 px-3 text-center font-mono text-xs relative">
                      <span
                        className={`group-hover:hidden ${
                          isCurrent ? 'hidden' : 'text-neutral-500'
                        }`}
                      >
                        {pt.position}
                      </span>
                      {isCurrent && !isPlaying && (
                        <Radio className="size-3.5 text-primary mx-auto group-hover:hidden" />
                      )}
                      {isPlaying && (
                        <Radio className="size-3.5 text-primary animate-pulse mx-auto group-hover:hidden" />
                      )}
                      <button
                        type="button"
                        onClick={(e) => {
                          e.stopPropagation()
                          handlePlayTrack(pt, index)
                        }}
                        className={`size-6 items-center justify-center rounded-full bg-primary text-white mx-auto transition-transform hover:scale-110 active:scale-95 ${
                          isCurrent ? 'flex' : 'hidden group-hover:flex'
                        }`}
                        title={isPlaying ? 'Pause' : 'Abspielen'}
                      >
                        {isPlaying ? (
                          <Pause className="size-3 fill-current" />
                        ) : (
                          <Play className="size-3 fill-current ml-0.5" />
                        )}
                      </button>
                    </td>

                    {/* Title */}
                    <td className="py-2.5 px-3 font-medium">
                      <div
                        className={`truncate max-w-[280px] lg:max-w-md ${
                          isCurrent ? 'text-primary font-semibold' : 'text-neutral-200'
                        }`}
                      >
                        {pt.title}
                      </div>
                    </td>

                    {/* Artist */}
                    <td className="py-2.5 px-3 text-neutral-400">
                      <div className="truncate max-w-[180px]">{artistName}</div>
                    </td>

                    {/* Album */}
                    <td className="py-2.5 px-3 text-neutral-400 hidden md:table-cell">
                      <div className="truncate max-w-[200px]">{pt.album || '–'}</div>
                    </td>

                    {/* Format */}
                    <td className="py-2.5 px-3 text-center">
                      {pt.codec ? (
                        <Badge
                          variant="outline"
                          className="font-mono text-[11px] uppercase py-0 px-1.5 h-5 border-white/10"
                        >
                          {pt.codec}
                        </Badge>
                      ) : (
                        <span className="text-neutral-600 text-xs">–</span>
                      )}
                    </td>

                    {/* Duration */}
                    <td className="py-2.5 px-3 text-right text-neutral-400 font-mono text-xs">
                      {formatDuration(pt.duration_ms)}
                    </td>

                    {/* Actions: Reorder Up/Down, Favorite, Delete */}
                    <td className="py-2.5 px-2 text-right">
                      <div className="flex items-center justify-end gap-1 opacity-60 group-hover:opacity-100 transition-opacity">
                        {/* Move Up */}
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={index === 0 || reordering}
                          className="h-7 w-7 p-0 text-neutral-400 hover:text-white disabled:opacity-20"
                          onClick={(e) => {
                            e.stopPropagation()
                            void handleMoveTrack(index, 'up')
                          }}
                          title="Nach oben verschieben"
                        >
                          <ArrowUp className="size-3.5" />
                        </Button>

                        {/* Move Down */}
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={index === tracks.length - 1 || reordering}
                          className="h-7 w-7 p-0 text-neutral-400 hover:text-white disabled:opacity-20"
                          onClick={(e) => {
                            e.stopPropagation()
                            void handleMoveTrack(index, 'down')
                          }}
                          title="Nach unten verschieben"
                        >
                          <ArrowDown className="size-3.5" />
                        </Button>

                        {/* Favorite */}
                        {favorites && (
                          <Button
                            variant="ghost"
                            size="sm"
                            className={`h-7 w-7 p-0 transition-colors ${
                              isFav
                                ? 'text-rose-500 hover:text-rose-400'
                                : 'text-neutral-400 hover:text-rose-400'
                            }`}
                            onClick={(e) => {
                              e.stopPropagation()
                              void favorites.toggleFavorite(pt.id)
                            }}
                            title={isFav ? 'Aus Favoriten entfernen' : 'Zu Favoriten hinzufügen'}
                          >
                            <Heart
                              className={`size-3.5 ${isFav ? 'fill-rose-500 text-rose-500' : ''}`}
                            />
                          </Button>
                        )}

                        {/* Remove from playlist */}
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={actionTrackId === pt.id}
                          className="h-7 w-7 p-0 text-neutral-400 hover:text-destructive hover:bg-destructive/10"
                          onClick={(e) => {
                            e.stopPropagation()
                            void handleRemoveTrack(pt.id)
                          }}
                          title="Aus Playlist entfernen"
                        >
                          {actionTrackId === pt.id ? (
                            <Loader2 className="size-3.5 animate-spin" />
                          ) : (
                            <Trash2 className="size-3.5" />
                          )}
                        </Button>
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

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

            <div className="space-y-4 py-4">
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
                <label className="text-xs font-semibold text-neutral-300">
                  Beschreibung
                </label>
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
              Möchtest du die Playlist &quot;{playlist.name}&quot; wirklich löschen?
              Die Titel in deiner Bibliothek bleiben davon unberührt.
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
