import { useCallback, useEffect, useRef, useState } from 'react'
import {
  AlertCircleIcon,
  BellIcon,
  BellRingIcon,
  Loader2Icon,
  RefreshCwIcon,
} from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Skeleton } from '@/components/ui/skeleton'
import { useJobEvents } from '@/hooks/useJobs'
import { errorMessage, isAbortError } from '@/lib/api/client'
import {
  findSubscription,
  subscribe as subscribeArtist,
  syncSubscription,
  updateSubscription,
} from '@/lib/api/subscriptions'
import { cn } from '@/lib/utils'
import type { Artist, JobEvent, Subscription } from '@/types/api'

interface SubscribeControlProps {
  artist: Artist
  className?: string
}

type Phase = 'loading' | 'ready' | 'busy'

/**
 * The subscribe control on the artist page.
 *
 * It owns its own state rather than taking it from the page: whether an artist
 * is watched is a question only this control asks, and threading it through
 * the page would make every artist view load subscription data it does not
 * otherwise need.
 *
 * The artist is watched on the provider it was found on. Nothing here converts
 * an artist onto a different provider.
 */
function SubscribeControl({ artist, className }: SubscribeControlProps) {
  const [subscription, setSubscription] = useState<Subscription | null>(null)
  const [phase, setPhase] = useState<Phase>('loading')
  const [error, setError] = useState<string | null>(null)

  const controllerRef = useRef<AbortController | null>(null)
  useEffect(() => () => controllerRef.current?.abort(), [])

  const provider = artist.provider
  const sourceId = artist.source_id

  // Load the current state whenever the artist changes.
  useEffect(() => {
    const controller = new AbortController()
    let active = true

    setPhase('loading')
    setError(null)

    findSubscription(provider, sourceId, controller.signal)
      .then((found) => {
        if (!active) return
        setSubscription(found)
        setPhase('ready')
      })
      .catch((cause: unknown) => {
        if (!active || isAbortError(cause)) return
        setError(errorMessage(cause))
        setPhase('ready')
      })

    return () => {
      active = false
      controller.abort()
    }
  }, [provider, sourceId])

  // A run that finishes elsewhere — the scheduler, another tab, the
  // subscriptions page — has to reach this button too.
  useJobEvents(
    useCallback(
      (event: JobEvent) => {
        if (!event.subscription_id || event.subscription_id !== subscription?.id) return
        if (event.type === 'subscription.sync.started') {
          setSubscription((current) => (current ? { ...current, syncing: true } : current))
          return
        }
        if (
          event.type === 'subscription.sync.completed' ||
          event.type === 'subscription.sync.failed'
        ) {
          setSubscription((current) =>
            current ? { ...current, syncing: false } : current,
          )
        }
      },
      [subscription?.id],
    ),
  )

  /** Runs one request, keeping the button honest about what it is doing. */
  async function perform(action: (signal: AbortSignal) => Promise<Subscription>) {
    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller

    setPhase('busy')
    setError(null)
    try {
      setSubscription(await action(controller.signal))
    } catch (cause) {
      if (isAbortError(cause)) return
      setError(errorMessage(cause))
    } finally {
      if (!controller.signal.aborted) setPhase('ready')
    }
  }

  if (phase === 'loading') {
    return <Skeleton className={cn('h-10 w-40 rounded-xl', className)} />
  }

  const busy = phase === 'busy'

  return (
    <div className={cn('flex flex-col gap-2', className)}>
      <div className="flex flex-wrap items-center gap-2">
        {subscription ? (
          <>
            <Button
              type="button"
              variant="accent"
              disabled={busy}
              onClick={() =>
                perform((signal) =>
                  updateSubscription(
                    subscription.id,
                    { enabled: !subscription.enabled },
                    signal,
                  ),
                )
              }
              title={
                subscription.enabled
                  ? 'Automatische Prüfung pausieren'
                  : 'Automatische Prüfung fortsetzen'
              }
            >
              <BellRingIcon />
              {subscription.enabled ? 'Abonniert' : 'Pausiert'}
            </Button>

            <Button
              type="button"
              variant="outline"
              disabled={busy || subscription.syncing}
              onClick={() =>
                perform((signal) => syncSubscription(subscription.id, signal))
              }
            >
              {subscription.syncing ? (
                <Loader2Icon className="animate-spin" />
              ) : (
                <RefreshCwIcon />
              )}
              {subscription.syncing ? 'Wird geprüft …' : 'Jetzt prüfen'}
            </Button>
          </>
        ) : (
          <Button
            type="button"
            variant="outline"
            disabled={busy}
            onClick={() =>
              perform((signal) =>
                subscribeArtist(
                  {
                    provider: artist.provider,
                    artist_source_id: artist.source_id,
                    artist_name: artist.name,
                    artist_image_url: artist.image_url,
                  },
                  signal,
                ),
              )
            }
          >
            {busy ? <Loader2Icon className="animate-spin" /> : <BellIcon />}
            Abonnieren
          </Button>
        )}
      </div>

      {subscription && (
        <label className="flex w-fit cursor-pointer items-center gap-2 text-sm text-muted-foreground">
          <Checkbox
            checked={subscription.auto_download}
            disabled={busy}
            onCheckedChange={(checked) =>
              perform((signal) =>
                updateSubscription(
                  subscription.id,
                  { auto_download: checked === true },
                  signal,
                ),
              )
            }
          />
          Neue Tracks automatisch herunterladen
        </label>
      )}

      {error && (
        <p role="alert" className="flex items-center gap-1.5 text-xs text-destructive">
          <AlertCircleIcon className="size-3.5 shrink-0" />
          {error}
        </p>
      )}
    </div>
  )
}

export { SubscribeControl }
