import { RadioIcon, WifiOffIcon } from 'lucide-react'

import { useConnectionState } from '@/hooks/useJobs'
import { cn } from '@/lib/utils'

/**
 * The state of the live event stream.
 *
 * Shown because the application stays usable when the stream drops — the views
 * simply stop updating by themselves — and a user needs to be able to tell the
 * difference between "nothing is happening" and "I am no longer being told
 * what happens".
 */
function ConnectionBadge({ className }: { className?: string }) {
  const state = useConnectionState()

  if (state === 'open') {
    return (
      <span
        className={cn(
          'inline-flex items-center gap-1.5 text-xs text-muted-foreground',
          className,
        )}
        title="Live-Updates sind verbunden"
      >
        <RadioIcon className="size-3.5 text-success" />
        <span className="hidden sm:inline">Live</span>
      </span>
    )
  }

  const connecting = state === 'connecting'

  return (
    <span
      role="status"
      className={cn(
        'inline-flex items-center gap-1.5 text-xs',
        connecting ? 'text-muted-foreground' : 'text-warning',
        className,
      )}
      title={
        connecting
          ? 'Live-Updates werden verbunden'
          : 'Keine Live-Updates — die Seite aktualisiert sich nicht von selbst'
      }
    >
      <WifiOffIcon className="size-3.5" />
      <span className="hidden sm:inline">
        {connecting ? 'Verbinde …' : 'Offline'}
      </span>
    </span>
  )
}

export { ConnectionBadge }
