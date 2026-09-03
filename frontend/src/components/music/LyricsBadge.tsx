import { FileTextIcon, Music2Icon, SparklesIcon } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import type { LyricsState } from '@/types/api'

interface LyricsBadgeProps {
  state?: LyricsState
  onClick?: () => void
  className?: string
}

export function LyricsBadge({ state, onClick, className }: LyricsBadgeProps) {
  if (!state || state === 'unknown' || state === 'not_found') {
    return null
  }

  const isClickable = Boolean(onClick)

  if (state === 'available_synced') {
    return (
      <Badge
        variant="success"
        className={cn(
          'gap-1 text-[10px] tracking-wide uppercase',
          isClickable && 'cursor-pointer hover:bg-success/20 transition-colors',
          className,
        )}
        onClick={onClick}
      >
        <SparklesIcon className="size-3" />
        Synced
      </Badge>
    )
  }

  if (state === 'available_plain') {
    return (
      <Badge
        variant="default"
        className={cn(
          'gap-1 text-[10px] tracking-wide uppercase',
          isClickable && 'cursor-pointer hover:bg-accent/80 transition-colors',
          className,
        )}
        onClick={onClick}
      >
        <FileTextIcon className="size-3" />
        Plain
      </Badge>
    )
  }

  if (state === 'instrumental') {
    return (
      <Badge
        variant="neutral"
        className={cn(
          'gap-1 text-[10px] tracking-wide uppercase',
          isClickable && 'cursor-pointer hover:bg-white/10 transition-colors',
          className,
        )}
        onClick={onClick}
      >
        <Music2Icon className="size-3" />
        Inst.
      </Badge>
    )
  }

  return null
}
