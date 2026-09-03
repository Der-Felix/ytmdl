import { useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { AlertCircleIcon, CheckIcon, DownloadIcon, Loader2Icon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { ApiError, errorMessage, isAbortError } from '@/lib/api/client'
import { cn } from '@/lib/utils'
import type { Job } from '@/types/api'
import type { buttonVariants } from '@/components/ui/button'
import type { VariantProps } from 'class-variance-authority'

type State =
  | { name: 'idle' }
  | { name: 'pending' }
  | { name: 'started' }
  | { name: 'error'; message: string; code?: string }

interface DownloadButtonProps {
  /** Creates the job. Receives an AbortSignal so an unmount cancels it. */
  start: (signal: AbortSignal) => Promise<Job>
  onStarted?: (job: Job) => void
  label?: ReactNode
  /** Shown after the job was accepted, instead of the label. */
  startedLabel?: ReactNode
  variant?: VariantProps<typeof buttonVariants>['variant']
  size?: VariantProps<typeof buttonVariants>['size']
  className?: string
  /** Renders the icon only, with the label as the accessible name. */
  iconOnly?: boolean
}

/**
 * Starts a download job and reports what happened.
 *
 * The backend answers 202 with the job — it does not wait for the download —
 * so "started" is the honest end state here, not "downloaded". A conflict
 * (ALREADY_EXISTS) is shown as information rather than as a failure, because
 * nothing went wrong: the music is already in the library.
 */
function DownloadButton({
  start,
  onStarted,
  label = 'Download',
  startedLabel = 'Gestartet',
  variant = 'outline',
  size = 'default',
  className,
  iconOnly = false,
}: DownloadButtonProps) {
  const [state, setState] = useState<State>({ name: 'idle' })
  const controllerRef = useRef<AbortController | null>(null)

  useEffect(() => () => controllerRef.current?.abort(), [])

  // The result is transient feedback on a button, not a permanent state.
  useEffect(() => {
    if (state.name !== 'started' && state.name !== 'error') return
    const timer = window.setTimeout(
      () => setState({ name: 'idle' }),
      state.name === 'error' ? 6000 : 3000,
    )
    return () => window.clearTimeout(timer)
  }, [state])

  async function handleClick() {
    if (state.name === 'pending') return

    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller
    setState({ name: 'pending' })

    try {
      const job = await start(controller.signal)
      setState({ name: 'started' })
      onStarted?.(job)
    } catch (error) {
      if (isAbortError(error)) return
      setState({
        name: 'error',
        message: errorMessage(error),
        code: error instanceof ApiError ? error.code : undefined,
      })
    }
  }

  const { icon, text, tone } = present(state, label, startedLabel)

  return (
    <div className={cn('inline-flex flex-col items-start gap-1', className)}>
      <Button
        type="button"
        variant={tone ?? variant}
        size={iconOnly ? (size === 'sm' ? 'icon-sm' : 'icon') : size}
        onClick={handleClick}
        disabled={state.name === 'pending'}
        aria-label={iconOnly ? textOf(label) : undefined}
        title={iconOnly ? textOf(label) : undefined}
      >
        {icon}
        {!iconOnly && text}
      </Button>
      {state.name === 'error' && !iconOnly && (
        <p role="alert" className="max-w-xs text-xs leading-snug text-destructive">
          {state.message}
        </p>
      )}
    </div>
  )
}

function present(
  state: State,
  label: ReactNode,
  startedLabel: ReactNode,
): {
  icon: ReactNode
  text: ReactNode
  tone?: VariantProps<typeof buttonVariants>['variant']
} {
  switch (state.name) {
    case 'pending':
      return { icon: <Loader2Icon className="animate-spin" />, text: 'Startet …' }
    case 'started':
      return { icon: <CheckIcon />, text: startedLabel, tone: 'accent' }
    case 'error':
      return { icon: <AlertCircleIcon />, text: 'Fehlgeschlagen', tone: 'destructive' }
    default:
      return { icon: <DownloadIcon />, text: label }
  }
}

/** A plain string for the accessible name of an icon-only button. */
function textOf(label: ReactNode): string {
  return typeof label === 'string' ? label : 'Download'
}

export { DownloadButton }
