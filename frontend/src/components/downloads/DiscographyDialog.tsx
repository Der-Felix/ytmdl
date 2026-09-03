import { useEffect, useId, useState } from 'react'
import { DownloadIcon, Loader2Icon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { DEFAULT_RELEASE_FILTER, RELEASE_TYPE_LABELS } from '@/lib/api/artists'
import { downloadArtist } from '@/lib/api/jobs'
import { errorMessage, isAbortError } from '@/lib/api/client'
import { formatNumber } from '@/lib/utils/format'
import type { Artist, Job, ReleaseFilter, ReleaseType } from '@/types/api'

/** The filter keys in the order they are offered, paired with their label. */
const FILTER_ROWS: { key: keyof ReleaseFilter; type: ReleaseType }[] = [
  { key: 'albums', type: 'album' },
  { key: 'singles', type: 'single' },
  { key: 'eps', type: 'ep' },
  { key: 'live', type: 'live' },
  { key: 'compilations', type: 'compilation' },
  { key: 'remixes', type: 'remix' },
]

interface DiscographyDialogProps {
  artist: Artist
  /** How many releases of each type exist, to label the rows honestly. */
  counts: Partial<Record<ReleaseType, number>>
  /** The server-wide default, used as the initial state of the skip switch. */
  defaultSkipExisting: boolean
  onStarted?: (job: Job) => void
}

/**
 * Picks what a discography download covers.
 *
 * Codec and bitrate are deliberately absent: the backend always takes the best
 * native Opus stream, and offering a choice the server does not act on would
 * be a lie in the interface.
 */
function DiscographyDialog({
  artist,
  counts,
  defaultSkipExisting,
  onStarted,
}: DiscographyDialogProps) {
  const [open, setOpen] = useState(false)
  const [filter, setFilter] = useState<ReleaseFilter>(DEFAULT_RELEASE_FILTER)
  const [skipExisting, setSkipExisting] = useState(defaultSkipExisting)
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const titleId = useId()

  // Reopening starts from the defaults rather than from whatever the last
  // attempt was left in.
  useEffect(() => {
    if (open) return
    setFilter(DEFAULT_RELEASE_FILTER)
    setSkipExisting(defaultSkipExisting)
    setError(null)
  }, [open, defaultSkipExisting])

  const selectedCount = FILTER_ROWS.filter((row) => filter[row.key]).reduce(
    (total, row) => total + (counts[row.type] ?? 0),
    0,
  )
  const nothingSelected = !FILTER_ROWS.some((row) => filter[row.key])

  async function handleSubmit() {
    if (nothingSelected || pending) return
    setPending(true)
    setError(null)

    try {
      const job = await downloadArtist({
        artist_id: artist.id,
        provider: artist.provider,
        release_filter: filter,
        skip_existing: skipExisting,
      })
      setOpen(false)
      onStarted?.(job)
    } catch (caught) {
      if (!isAbortError(caught)) setError(errorMessage(caught))
    } finally {
      setPending(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <Button variant="default" size="lg" onClick={() => setOpen(true)}>
        <DownloadIcon />
        Diskografie herunterladen
      </Button>

      <DialogContent aria-labelledby={titleId}>
        <DialogHeader>
          <DialogTitle id={titleId}>Diskografie herunterladen</DialogTitle>
          <DialogDescription>
            Welche Veröffentlichungen von {artist.name} sollen geladen werden?
          </DialogDescription>
        </DialogHeader>

        <fieldset className="space-y-1">
          <legend className="sr-only">Releasetypen</legend>
          {FILTER_ROWS.map((row) => {
            const count = counts[row.type]
            const unavailable = count === undefined || count === 0

            return (
              <label
                key={row.key}
                className="flex cursor-pointer items-center gap-3 rounded-xl px-2 py-2.5 transition-colors hover:bg-white/4 has-disabled:cursor-not-allowed has-disabled:opacity-45"
              >
                <Checkbox
                  checked={filter[row.key]}
                  disabled={unavailable}
                  onCheckedChange={(checked) =>
                    setFilter((current) => ({ ...current, [row.key]: checked === true }))
                  }
                />
                <span className="flex-1 text-sm text-foreground">
                  {RELEASE_TYPE_LABELS[row.type].many}
                </span>
                <span className="text-xs tabular-nums text-muted-foreground">
                  {unavailable ? 'keine' : formatNumber(count)}
                </span>
              </label>
            )
          })}
        </fieldset>

        <label className="flex cursor-pointer items-start gap-3 rounded-xl border border-border bg-white/3 px-3.5 py-3">
          <Checkbox
            checked={skipExisting}
            onCheckedChange={(checked) => setSkipExisting(checked === true)}
            className="mt-0.5"
          />
          <span className="space-y-0.5">
            <span className="block text-sm text-foreground">
              Bereits vorhandene Tracks überspringen
            </span>
            <span className="block text-xs leading-relaxed text-muted-foreground">
              Tracks, die schon in der Bibliothek liegen, werden nicht erneut
              geladen.
            </span>
          </span>
        </label>

        {error && (
          <p
            role="alert"
            className="rounded-xl border border-destructive/20 bg-destructive/8 px-3.5 py-2.5 text-xs leading-relaxed text-destructive"
          >
            {error}
          </p>
        )}

        <DialogFooter>
          <DialogClose render={<Button variant="ghost" />}>Abbrechen</DialogClose>
          <Button
            variant="default"
            onClick={handleSubmit}
            disabled={nothingSelected || pending}
          >
            {pending ? <Loader2Icon className="animate-spin" /> : <DownloadIcon />}
            {selectedCount > 0
              ? `${formatNumber(selectedCount)} Releases laden`
              : 'Herunterladen'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export { DiscographyDialog }
