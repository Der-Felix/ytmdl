import { useCallback, useState } from 'react'
import {
  ListMusic,
  Loader2,
  Play,
  Plus,
} from 'lucide-react'

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
import { formatRelative } from '@/lib/utils/format'
import type { Playlist } from '@/types/playlist'

export function Playlists() {
  const navigate = useNavigate()
  const { playAlbum } = usePlayerActions()
  const { state, reload, setData } = useAsync((signal) => listPlaylists(signal), [])

  const [createDialogOpen, setCreateDialogOpen] = useState(false)
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [playingPlaylistId, setPlayingPlaylistId] = useState<string | null>(null)

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!name.trim()) return

    try {
      setCreating(true)
      setCreateError(null)
      const created = await createPlaylist({
        name: name.trim(),
        description: description.trim(),
      })
      setData((prev) => (prev ? [created, ...prev] : [created]))
      setCreateDialogOpen(false)
      setName('')
      setDescription('')
      navigate(paths.playlist(created.id))
    } catch (err: unknown) {
      setCreateError(
        err instanceof Error ? err.message : 'Fehler beim Erstellen der Playlist',
      )
    } finally {
      setCreating(false)
    }
  }

  const handlePlayPlaylist = useCallback(
    async (e: React.MouseEvent, playlist: Playlist) => {
      e.stopPropagation()
      e.preventDefault()
      try {
        setPlayingPlaylistId(playlist.id)
        const detail = await getPlaylist(playlist.id)
        if (detail.tracks && detail.tracks.length > 0) {
          playAlbum(detail.tracks)
        }
      } catch {
        // Play failure silently ignored or covered by player error handling
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
          <p className="text-sm text-neutral-400 mt-1">
            Deine persönlichen Wiedergabelisten
          </p>
        </div>
        <Button
          onClick={() => {
            setName('')
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

      {state.status === 'error' && (
        <ErrorState error={state.error} onRetry={reload} />
      )}

      {state.status === 'success' && state.data.length === 0 && (
        <EmptyState
          icon={<ListMusic />}
          title="Keine Playlists vorhanden"
          description="Erstelle deine erste Playlist, um deine Lieblingstitel individuell zu organisieren."
          action={
            <Button
              onClick={() => {
                setName('')
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

      {state.status === 'success' && state.data.length > 0 && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
          {state.data.map((playlist) => (
            <Link
              key={playlist.id}
              href={paths.playlist(playlist.id)}
              className="group relative flex flex-col rounded-2xl border border-white/5 bg-white/[0.02] p-4 transition-all hover:border-white/10 hover:bg-white/[0.05] hover:shadow-xl"
            >
              <div className="relative aspect-video w-full rounded-xl bg-gradient-to-br from-neutral-900 to-neutral-800 border border-white/5 flex items-center justify-center overflow-hidden mb-3.5">
                <ListMusic className="size-12 text-neutral-600 transition-transform group-hover:scale-110" />

                {playlist.track_count > 0 && (
                  <Button
                    size="icon"
                    onClick={(e) => void handlePlayPlaylist(e, playlist)}
                    disabled={playingPlaylistId === playlist.id}
                    className="absolute right-3 bottom-3 size-10 rounded-full bg-primary text-white shadow-lg opacity-0 group-hover:opacity-100 transition-all duration-200 hover:scale-105 active:scale-95"
                    title="Playlist abspielen"
                    aria-label="Playlist abspielen"
                  >
                    {playingPlaylistId === playlist.id ? (
                      <Loader2 className="size-5 animate-spin" />
                    ) : (
                      <Play className="size-5 fill-current ml-0.5" />
                    )}
                  </Button>
                )}
              </div>

              <div className="flex-1 min-w-0">
                <h3 className="font-semibold text-base text-neutral-100 truncate group-hover:text-primary transition-colors">
                  {playlist.name}
                </h3>
                {playlist.description && (
                  <p className="text-xs text-neutral-400 line-clamp-2 mt-0.5">
                    {playlist.description}
                  </p>
                )}
              </div>

              <div className="mt-3.5 pt-3 border-t border-white/5 flex items-center justify-between text-xs text-neutral-500 font-mono">
                <span>
                  {playlist.track_count}{' '}
                  {playlist.track_count === 1 ? 'Titel' : 'Titel'}
                </span>
                <span>{formatRelative(playlist.updated_at)}</span>
              </div>
            </Link>
          ))}
        </div>
      )}

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

            <div className="space-y-4 py-4">
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
                {creating ? (
                  <Loader2 className="size-4 animate-spin mr-2" />
                ) : null}
                Erstellen
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}
