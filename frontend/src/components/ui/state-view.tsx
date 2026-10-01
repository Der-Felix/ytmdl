import type { ReactNode } from 'react'
import { RotateCwIcon } from 'lucide-react'

import { ApiError } from '@/lib/api/client'
import { explainProblem } from '@/lib/problems'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { ProblemNotice } from '@/components/ui/problem-notice'

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
 * Explain the cause and next step; technical identifiers can be expanded.
 */
function ErrorState({ error, onRetry, className }: ErrorStateProps) {
  const code = error instanceof ApiError ? error.code : undefined
  const status = error instanceof ApiError ? error.status : undefined
  const info = explainProblem(code, status)

  return (
    <div
      role="alert"
      className={cn(
        'mx-auto max-w-2xl px-4 py-6 sm:px-6',
        className,
      )}
    >
      <ProblemNotice
        code={code}
        status={status}
        requestId={error instanceof ApiError ? error.requestId : undefined}
        waiting={['PROVIDER_RATE_LIMITED', 'RATE_LIMITED', 'SESSION_UNAVAILABLE', 'SESSION_RATE_LIMITED', 'SESSION_BOT_CHALLENGE', 'SESSION_IN_USE'].includes(code || '')}
        action={onRetry && info.retryable ? (
          <Button variant="outline" size="sm" onClick={onRetry}>
            <RotateCwIcon />
            Erneut versuchen
          </Button>
        ) : undefined}
      />
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
