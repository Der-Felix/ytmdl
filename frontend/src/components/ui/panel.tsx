import type { ComponentProps, ReactNode } from 'react'

import { cn } from '@/lib/utils'

/**
 * The translucent surface every section of the application is built from.
 * Nothing else in the codebase spells out the card background and border.
 */
function Panel({ className, ...props }: ComponentProps<'div'>) {
  return <div data-slot="panel" className={cn('panel', className)} {...props} />
}

interface PanelHeaderProps extends Omit<ComponentProps<'div'>, 'title'> {
  title: ReactNode
  description?: ReactNode
  /** Rendered at the trailing edge — a link, a filter, a count. */
  action?: ReactNode
}

function PanelHeader({
  title,
  description,
  action,
  className,
  ...props
}: PanelHeaderProps) {
  return (
    <div
      data-slot="panel-header"
      className={cn('flex items-start justify-between gap-4', className)}
      {...props}
    >
      <div className="min-w-0 space-y-1">
        <h2 className="font-heading text-[0.9375rem] font-semibold text-foreground">
          {title}
        </h2>
        {description && (
          <p className="text-sm text-muted-foreground">{description}</p>
        )}
      </div>
      {action && <div className="shrink-0">{action}</div>}
    </div>
  )
}

/** A section heading with a hairline, used to separate release groups. */
function SectionHeading({
  title,
  count,
  className,
  ...props
}: Omit<ComponentProps<'div'>, 'title'> & { title: ReactNode; count?: ReactNode }) {
  return (
    <div
      className={cn('flex items-baseline gap-3 pb-1', className)}
      {...props}
    >
      <h2 className="font-heading text-lg font-semibold text-foreground">
        {title}
      </h2>
      {count !== undefined && (
        <span className="text-sm text-muted-foreground tabular-nums">{count}</span>
      )}
      <span aria-hidden className="h-px flex-1 bg-border" />
    </div>
  )
}

export { Panel, PanelHeader, SectionHeading }
