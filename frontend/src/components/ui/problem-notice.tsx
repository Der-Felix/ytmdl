import { AlertCircleIcon, InfoIcon } from 'lucide-react'
import type { ReactNode } from 'react'

import { explainProblem } from '@/lib/problems'
import { cn } from '@/lib/utils'

interface ProblemNoticeProps {
  code?: string
  status?: number
  requestId?: string
  waiting?: boolean
  continuation?: string
  action?: ReactNode
  compact?: boolean
  className?: string
}

/** Technical identifiers remain available; raw server/provider output is not rendered. */
export function ProblemNotice({ code, status, requestId, waiting = false, continuation, action, compact = false, className }: ProblemNoticeProps) {
  const info = explainProblem(code, status)
  const Icon = waiting ? InfoIcon : AlertCircleIcon
  const safeCode = code && /^[A-Z0-9_]{1,64}$/.test(code) ? code : undefined
  const safeRequestId = requestId && /^[\w-]{1,128}$/.test(requestId) ? requestId : undefined

  return (
    <div className={cn('rounded-xl border', compact ? 'p-2.5' : 'p-3 sm:p-4', waiting ? 'border-amber-500/20 bg-amber-500/5' : 'border-destructive/20 bg-destructive/5', className)}>
      <div className="flex items-start gap-3">
        <Icon aria-hidden className={cn('mt-0.5 size-4 shrink-0', waiting ? 'text-amber-600 dark:text-amber-400' : 'text-destructive')} />
        <div className="min-w-0 flex-1 space-y-2 text-left">
          <p className={cn('font-medium leading-relaxed text-foreground', compact ? 'text-xs' : 'text-sm')}>{info.title}</p>
          {!compact && <p className="text-xs leading-relaxed text-muted-foreground">{info.explanation}</p>}
          <p className="text-xs leading-relaxed text-foreground">{info.nextStep}</p>
          {continuation && <p className="text-xs font-medium text-amber-600 dark:text-amber-400">{continuation}</p>}
          {action}
          {(safeCode || safeRequestId) && (
            <details className="pt-1 text-xs text-muted-foreground">
              <summary className="w-fit cursor-pointer rounded-sm focus-visible:outline-2 focus-visible:outline-ring">Technische Details</summary>
              <dl className="mt-2 space-y-1 break-all font-mono text-[0.6875rem]">
                {safeCode && <div><dt className="inline">Fehlercode: </dt><dd className="inline">{safeCode}</dd></div>}
                {safeRequestId && <div><dt className="inline">Anfrage-ID: </dt><dd className="inline">{safeRequestId}</dd></div>}
              </dl>
            </details>
          )}
        </div>
      </div>
    </div>
  )
}
