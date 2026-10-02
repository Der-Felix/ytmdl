import { useState } from 'react'
import { DiscIcon, UserIcon } from 'lucide-react'

import { cn } from '@/lib/utils'

interface CoverProps {
  src?: string
  fallbackSrc?: string
  alt: string
  /** A round frame for artists, a rounded square for releases. */
  shape?: 'square' | 'circle'
  className?: string
}

/**
 * Artwork with a fallback.
 *
 * Provider cover URLs go stale, and YouTube Music delivers none at all for
 * some releases, so a missing or broken image is the normal case rather than
 * an error: the placeholder keeps the grid aligned instead of collapsing the
 * tile.
 */
function Cover(props: CoverProps) {
  return <CoverImage key={`${props.src ?? ''}|${props.fallbackSrc ?? ''}`} {...props} />
}

function CoverImage({ src, fallbackSrc, alt, shape = 'square', className }: CoverProps) {
  const sources = [...new Set([src, fallbackSrc].filter((value): value is string => Boolean(value)))]
  const [attempt, setAttempt] = useState(0)

  const rounded = shape === 'circle' ? 'rounded-full' : 'rounded-2xl'
  const showImage = attempt < sources.length

  return (
    <div
      className={cn(
        'relative isolate aspect-square overflow-hidden border border-border bg-white/4',
        rounded,
        className,
      )}
    >
      {showImage ? (
        <img
          src={sources[attempt]}
          alt={alt}
          loading="lazy"
          decoding="async"
          onError={() => setAttempt((value) => value + 1)}
          className="size-full object-cover"
        />
      ) : (
        <div
          className="flex size-full items-center justify-center bg-gradient-to-br from-white/6 to-transparent text-muted-foreground/50"
          aria-hidden
        >
          {shape === 'circle' ? (
            <UserIcon className="size-1/3" strokeWidth={1.5} />
          ) : (
            <DiscIcon className="size-1/3" strokeWidth={1.5} />
          )}
        </div>
      )}
    </div>
  )
}

export { Cover }
