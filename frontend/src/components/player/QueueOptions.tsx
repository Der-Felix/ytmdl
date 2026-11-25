import { useId, useState } from 'react'
import type { FormEvent } from 'react'
import { Switch } from '@base-ui/react/switch'
import { ListPlus, Loader2 } from 'lucide-react'

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
import { usePlayerActions, usePlayerState } from '@/hooks/usePlayer'
import { saveQueueAsPlaylist } from '@/lib/queue-playlist'
import { notifyWithAction } from '@/lib/notify'
import { navigate, paths } from '@/lib/router'
import { cn } from '@/lib/utils'

/*
 * The queue's own settings, under its header in the drawer and on the player
 * page: whether playback carries on with similar tracks when the queue runs
 * out, and saving the queue as a playlist.
 */

export function QueueOptions({ className }: { className?: string }) {
  const { queue } = usePlayerState()
  const [saveOpen, setSaveOpen] = useState(false)
  return (
    <div className={cn('flex flex-wrap items-center justify-between gap-x-3 gap-y-1', className)}>
      <AutoplaySwitch />
      <Button
        variant="ghost"
        size="sm"
        disabled={queue.length === 0}
        onClick={() => setSaveOpen(true)}
        className="h-9 px-2.5 text-xs text-muted-foreground hover:text-foreground"
      >
        <ListPlus className="size-3.5" aria-hidden="true" />
        Als Playlist speichern
      </Button>
      <SaveQueueDialog open={saveOpen} onOpenChange={setSaveOpen} />
    </div>
  )
}

export function AutoplaySwitch() {
  const { autoplay } = usePlayerState()
  const { setAutoplay } = usePlayerActions()
  const hint = useId()
  return (
    <label
      className="flex min-h-9 cursor-pointer items-center gap-2.5 rounded-md text-xs font-medium text-foreground"
      title="Spielt am Ende der Warteschlange ähnliche Titel aus deiner Bibliothek"
    >
      <Switch.Root
        checked={autoplay}
        onCheckedChange={(checked) => setAutoplay(checked)}
        aria-describedby={hint}
        className={cn(
          'relative inline-flex h-5 w-9 shrink-0 items-center rounded-full border border-border bg-input p-0.5 outline-none',
          'transition-[background-color,border-color] duration-(--duration-fast) ease-(--ease-out)',
          'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
          'data-[checked]:border-primary data-[checked]:bg-primary',
        )}
      >
        <Switch.Thumb
          className={cn(
            'block size-3.5 rounded-full bg-muted-foreground shadow-sm',
            'transition-[transform,background-color] duration-(--duration-fast) ease-(--ease-out)',
            'data-[checked]:translate-x-4 data-[checked]:bg-primary-foreground',
          )}
        />
      </Switch.Root>
      <span>Automatisch weiterspielen</span>
      <span id={hint} className="sr-only">
        Spielt am Ende der Warteschlange ähnliche Titel aus deiner Bibliothek.
      </span>
    </label>
  )
}

const encoder = new TextEncoder()

/** The server allows 128 bytes; umlauts count twice. */
function nameError(name: string) {
  const trimmed = name.trim()
  if (!trimmed) return 'Bitte gib der Playlist einen Namen.'
  if (encoder.encode(trimmed).length > 128) return 'Der Name ist zu lang.'
  return ''
}

function defaultName(title: string | undefined) {
  const date = new Intl.DateTimeFormat('de-DE', { day: '2-digit', month: '2-digit', year: 'numeric' }).format(new Date())
  return `${title ?? 'Warteschlange'} · ${date}`
}

/** Something other than the queue to save, such as a mix. */
export interface PlaylistSource {
  /** Names the playlist by default: "{title} · dd.mm.yyyy". */
  title: string
  trackIds: string[]
  /** The dialog heading, e.g. "Mix als Playlist speichern". */
  heading: string
  /** How the order is described: "in der Reihenfolge des Mixes". */
  orderNote: string
}

export function SaveQueueDialog({
  open,
  onOpenChange,
  save = saveQueueAsPlaylist,
  source,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Creates and fills the playlist; replaceable in tests. */
  save?: typeof saveQueueAsPlaylist
  /** Saves these titles instead of the queue. */
  source?: PlaylistSource
}) {
  const { queue, queueContext } = usePlayerState()
  const [name, setName] = useState('')
  const [touched, setTouched] = useState(false)
  const [saving, setSaving] = useState(false)
  const [failure, setFailure] = useState('')
  const inputId = useId()
  const errorId = useId()

  const trackIds = [...new Set(source ? source.trackIds : queue.map((t) => t.id))]
  const error = touched ? nameError(name) : ''

  // A fresh form every time the dialog opens, named after what plays.
  const [wasOpen, setWasOpen] = useState(false)
  if (open !== wasOpen) {
    setWasOpen(open)
    if (open) {
      setName(defaultName(source ? source.title : queueContext?.title))
      setTouched(false)
      setFailure('')
    }
  }

  const handleOpenChange = (next: boolean) => {
    if (saving) return
    onOpenChange(next)
  }

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    setTouched(true)
    if (nameError(name) || trackIds.length === 0 || saving) return
    setSaving(true)
    setFailure('')
    try {
      const playlist = await save(name.trim(), trackIds)
      onOpenChange(false)
      notifyWithAction(
        'Als Playlist gespeichert',
        { label: 'Öffnen', onClick: () => navigate(paths.playlist(playlist.id)) },
        { description: `${playlist.name} · ${trackIds.length} Titel` },
      )
    } catch (err) {
      setFailure(err instanceof Error ? err.message : 'Die Playlist konnte nicht gespeichert werden.')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{source?.heading ?? 'Warteschlange als Playlist speichern'}</DialogTitle>
          <DialogDescription>
            {trackIds.length === 1 ? '1 Titel' : `${trackIds.length} Titel`} {source?.orderNote ?? 'in der Reihenfolge der Warteschlange'}.
          </DialogDescription>
        </DialogHeader>
        <form id={`${inputId}-form`} onSubmit={(e) => void submit(e)} className="space-y-1.5">
          <label htmlFor={inputId} className="text-xs font-medium text-foreground">
            Name
          </label>
          <Input
            id={inputId}
            autoFocus
            value={name}
            maxLength={100}
            disabled={saving}
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? errorId : undefined}
            onChange={(e) => setName(e.target.value)}
            onBlur={() => setTouched(true)}
          />
          {error && (
            <p id={errorId} className="text-xs text-destructive">
              {error}
            </p>
          )}
          {failure && (
            <p role="alert" className="rounded-lg border border-destructive/20 bg-destructive/10 p-2.5 text-xs text-destructive">
              {failure}
            </p>
          )}
        </form>
        <DialogFooter>
          <Button type="button" variant="ghost" disabled={saving} onClick={() => handleOpenChange(false)}>
            Abbrechen
          </Button>
          <Button type="submit" form={`${inputId}-form`} disabled={saving || trackIds.length === 0}>
            {saving && <Loader2 className="size-4 animate-spin" aria-hidden="true" />}
            Speichern
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
