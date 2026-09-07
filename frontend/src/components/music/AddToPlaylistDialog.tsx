import { useEffect, useState } from 'react'
import { Check, ListMusic, Loader2, Plus } from 'lucide-react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { ApiError } from '@/lib/api/client'
import { addTrackToPlaylist, createPlaylist, listPlaylists } from '@/lib/api/playlists'
import type { LibraryTrack } from '@/types/api'
import type { Playlist } from '@/types/playlist'

interface AddToPlaylistDialogProps {
  track: LibraryTrack | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function AddToPlaylistDialog({
  track,
  open,
  onOpenChange,
}: AddToPlaylistDialogProps) {
  const [playlists, setPlaylists] = useState<Playlist[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Track addition state per playlist id
  const [addingId, setAddingId] = useState<string | null>(null)
  const [addedIds, setAddedIds] = useState<Set<string>>(new Set())

  // New playlist creation state
  const [isCreating, setIsCreating] = useState(false)
  const [newPlaylistName, setNewPlaylistName] = useState('')
  const [createLoading, setCreateLoading] = useState(false)

  useEffect(() => {
    if (!open) {
      setAddingId(null)
      setAddedIds(new Set())
      setIsCreating(false)
      setNewPlaylistName('')
      setError(null)
      return
    }

    async function load() {
      try {
        setLoading(true)
        setError(null)
        const list = await listPlaylists()
        setPlaylists(list)
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Fehler beim Laden der Playlists')
      } finally {
        setLoading(false)
      }
    }

    void load()
  }, [open])

  const handleAddToPlaylist = async (playlistId: string) => {
    if (!track) return
    try {
      setAddingId(playlistId)
      setError(null)
      await addTrackToPlaylist(playlistId, track.id)
      setAddedIds((prev) => new Set(prev).add(playlistId))
      // Auto-close dialog after brief feedback
      setTimeout(() => {
        onOpenChange(false)
      }, 700)
    } catch (err: unknown) {
      if (err instanceof ApiError && err.code === 'ALREADY_EXISTS') {
        setError('Track ist bereits in dieser Playlist')
      } else {
        const msg = err instanceof Error ? err.message : 'Fehler beim Hinzufügen'
        if (msg.includes('already exists') || msg.includes('ALREADY_EXISTS') || msg.includes('bereits in dieser Playlist')) {
          setError('Track ist bereits in dieser Playlist')
        } else {
          setError(msg)
        }
      }
    } finally {
      setAddingId(null)
    }
  }

  const handleCreateAndAdd = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!track || !newPlaylistName.trim()) return

    try {
      setCreateLoading(true)
      setError(null)
      const created = await createPlaylist({ name: newPlaylistName.trim() })
      await addTrackToPlaylist(created.id, track.id)
      setAddedIds((prev) => new Set(prev).add(created.id))
      setPlaylists((prev) => [created, ...prev])
      setNewPlaylistName('')
      setIsCreating(false)
      setTimeout(() => {
        onOpenChange(false)
      }, 700)
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Fehler beim Erstellen der Playlist')
    } finally {
      setCreateLoading(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ListMusic className="size-5 text-primary" />
            Zur Playlist hinzufügen
          </DialogTitle>
          <DialogDescription className="truncate">
            {track ? `"${track.title}" auswählen` : 'Wähle eine Playlist aus'}
          </DialogDescription>
        </DialogHeader>

        {error && (
          <div className="rounded-lg bg-destructive/10 border border-destructive/20 p-2.5 text-xs text-destructive">
            {error}
          </div>
        )}

        {loading ? (
          <div className="py-8 flex justify-center items-center">
            <Loader2 className="size-6 animate-spin text-muted-foreground" />
          </div>
        ) : (
          <div className="space-y-4 max-h-72 overflow-y-auto pr-1">
            {playlists.length === 0 && !isCreating ? (
              <div className="text-center py-6 text-sm text-muted-foreground">
                <p>Du hast noch keine Playlists.</p>
              </div>
            ) : (
              <div className="space-y-1.5">
                {playlists.map((pl) => {
                  const isAdded = addedIds.has(pl.id)
                  const isAdding = addingId === pl.id

                  return (
                    <button
                      key={pl.id}
                      type="button"
                      disabled={isAdding || isAdded}
                      onClick={() => void handleAddToPlaylist(pl.id)}
                      className={`w-full flex items-center justify-between p-2.5 rounded-lg border text-left transition-colors ${
                        isAdded
                          ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-300'
                          : 'border-white/5 bg-white/[0.02] hover:bg-white/[0.06] text-neutral-200'
                      }`}
                    >
                      <div className="truncate mr-2">
                        <div className="font-medium text-sm truncate">{pl.name}</div>
                        <div className="text-xs text-neutral-500">
                          {pl.track_count} {pl.track_count === 1 ? 'Track' : 'Tracks'}
                        </div>
                      </div>
                      <div>
                        {isAdding ? (
                          <Loader2 className="size-4 animate-spin text-primary" />
                        ) : isAdded ? (
                          <Check className="size-4 text-emerald-400" />
                        ) : (
                          <Plus className="size-4 text-neutral-400" />
                        )}
                      </div>
                    </button>
                  )
                })}
              </div>
            )}

            {isCreating ? (
              <form onSubmit={(e) => void handleCreateAndAdd(e)} className="space-y-2 pt-2 border-t border-white/10">
                <Input
                  autoFocus
                  placeholder="Playlist-Name..."
                  value={newPlaylistName}
                  onChange={(e) => setNewPlaylistName(e.target.value)}
                  disabled={createLoading}
                  maxLength={100}
                />
                <div className="flex justify-end gap-2">
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    disabled={createLoading}
                    onClick={() => setIsCreating(false)}
                  >
                    Abbrechen
                  </Button>
                  <Button
                    type="submit"
                    size="sm"
                    disabled={createLoading || !newPlaylistName.trim()}
                  >
                    {createLoading ? <Loader2 className="size-3.5 animate-spin mr-1.5" /> : null}
                    Erstellen & Hinzufügen
                  </Button>
                </div>
              </form>
            ) : (
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="w-full mt-2"
                onClick={() => setIsCreating(true)}
              >
                <Plus className="size-4 mr-1.5" />
                Neue Playlist erstellen
              </Button>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
