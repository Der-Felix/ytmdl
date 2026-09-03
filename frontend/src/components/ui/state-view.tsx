import type { ReactNode } from 'react'
import { AlertTriangleIcon, RotateCwIcon } from 'lucide-react'

import { ApiError, errorMessage } from '@/lib/api/client'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'

/**
 * The three non-success states, in one place. Every data-driven view in the
 * application renders through these, so no view can end up as a blank area or
 * as raw JSON on screen.
 */

interface EmptyStateProps {
  icon?: ReactNode
  title: string
  description?: ReactNode
  action?: ReactNode
  className?: string
}

function EmptyState({
  icon,
  title,
  description,
  action,
  className,
}: EmptyStateProps) {
  return (
    <div
      className={cn(
        'flex flex-col items-center justify-center gap-3 px-6 py-10 text-center',
        className,
      )}
    >
      {icon && (
        <div className="flex size-11 items-center justify-center rounded-xl border border-border bg-white/4 text-muted-foreground [&_svg]:size-5">
          {icon}
        </div>
      )}
      <div className="space-y-1">
        <p className="font-heading text-[0.9375rem] font-medium text-foreground">
          {title}
        </p>
        {description && (
          <p className="mx-auto max-w-md text-sm leading-relaxed text-muted-foreground">
            {description}
          </p>
        )}
      </div>
      {action}
    </div>
  )
}

interface ErrorStateProps {
  error: unknown
  /** Omitted when the failure is not worth retrying. */
  onRetry?: () => void
  className?: string
}

/**
 * A failure the user can act on. The backend's error code is shown alongside
 * the message because it is stable and makes a report unambiguous — but the
 * message itself is what the user reads.
 */
function ErrorState({ error, onRetry, className }: ErrorStateProps) {
  const code = error instanceof ApiError ? error.code : undefined
  const retryable = !(error instanceof ApiError) || error.isRetryable

  return (
    <div
      role="alert"
      className={cn(
        'flex flex-col items-center justify-center gap-3 px-6 py-10 text-center',
        className,
      )}
    >
      <div className="flex size-11 items-center justify-center rounded-xl border border-destructive/25 bg-destructive/10 text-destructive">
        <AlertTriangleIcon className="size-5" />
      </div>
      <div className="space-y-1">
        <p className="font-heading text-[0.9375rem] font-medium text-foreground">
          {errorMessage(error)}
        </p>
        {code && (
          <p className="font-mono text-xs tracking-wide text-muted-foreground">
            {code}
          </p>
        )}
      </div>
      {onRetry && retryable && (
        <Button variant="outline" size="sm" onClick={onRetry}>
          <RotateCwIcon />
          Erneut versuchen
        </Button>
      )}
    </div>
  )
}

/** Placeholder rows for a list that is still loading. */
function ListSkeleton({ rows = 4, className }: { rows?: number; className?: string }) {
  return (
    <div className={cn('space-y-2.5', className)} aria-hidden>
      {Array.from({ length: rows }, (_, index) => (
        <Skeleton key={index} className="h-16 w-full rounded-xl" />
      ))}
    </div>
  )
}

/** Placeholder tiles for a cover grid that is still loading. */
function GridSkeleton({ tiles = 6, className }: { tiles?: number; className?: string }) {
  return (
    <div
      className={cn(
        'grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5',
        className,
      )}
      aria-hidden
    >
      {Array.from({ length: tiles }, (_, index) => (
        <div key={index} className="space-y-3">
          <Skeleton className="aspect-square w-full rounded-2xl" />
          <Skeleton className="h-3.5 w-3/4 rounded-md" />
          <Skeleton className="h-3 w-1/2 rounded-md" />
        </div>
      ))}
    </div>
  )
}

/** Announces a loading region to assistive technology. */
function LoadingRegion({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div role="status" aria-live="polite" aria-busy="true">
      <span className="sr-only">{label}</span>
      {children}
    </div>
  )
}

export { EmptyState, ErrorState, GridSkeleton, ListSkeleton, LoadingRegion }
