import { useEffect, useState } from 'react'
import { DiscIcon, UserIcon } from 'lucide-react'

import { cn } from '@/lib/utils'

interface CoverProps {
  src?: string
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
function Cover({ src, alt, shape = 'square', className }: CoverProps) {
  const [failed, setFailed] = useState(false)

  // A new URL deserves a fresh attempt; without this a once-broken image would
  // stay broken after the component is reused for another release.
  useEffect(() => setFailed(false), [src])

  const rounded = shape === 'circle' ? 'rounded-full' : 'rounded-2xl'
  const showImage = Boolean(src) && !failed

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
          src={src}
          alt={alt}
          loading="lazy"
          decoding="async"
          onError={() => setFailed(true)}
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
