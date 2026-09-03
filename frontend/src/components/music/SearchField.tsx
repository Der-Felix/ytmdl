import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { SearchIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { paths, useNavigate } from '@/lib/router'
import { cn } from '@/lib/utils'

interface SearchFieldProps {
  /** Prefills the field, so a reloaded /discover?q= keeps its query. */
  defaultValue?: string
  /** The oversized variant used on the dashboard. */
  size?: 'default' | 'hero'
  autoFocus?: boolean
  className?: string
}

/**
 * The single entry point for finding music: a name, or a link to an artist or
 * album. Submitting navigates to /discover, which is also what decides whether
 * the input was a query or a link — so a bookmarked /discover?q= behaves the
 * same as typing here.
 */
function SearchField({
  defaultValue = '',
  size = 'default',
  autoFocus,
  className,
}: SearchFieldProps) {
  const [value, setValue] = useState(defaultValue)
  const navigate = useNavigate()

  // Keep the field in step when the query changes from outside, e.g. by
  // pressing back into an earlier search.
  useEffect(() => setValue(defaultValue), [defaultValue])

  function handleSubmit(event: FormEvent) {
    event.preventDefault()
    const query = value.trim()
    if (query) navigate(paths.discover(query))
  }

  const hero = size === 'hero'

  return (
    <form
      role="search"
      onSubmit={handleSubmit}
      className={cn('flex w-full items-center gap-2', className)}
    >
      <div className="relative min-w-0 flex-1">
        <SearchIcon
          aria-hidden
          className={cn(
            'pointer-events-none absolute top-1/2 left-4 size-4 -translate-y-1/2 text-muted-foreground',
            hero && 'size-[1.125rem] left-5',
          )}
        />
        <input
          type="search"
          name="q"
          value={value}
          autoFocus={autoFocus}
          onChange={(event) => setValue(event.target.value)}
          aria-label="Künstler, Album oder URL suchen"
          placeholder="Künstler, Album oder URL suchen"
          className={cn(
            'w-full rounded-2xl border border-input bg-white/4 text-foreground transition-colors outline-none',
            'placeholder:text-muted-foreground',
            'hover:border-white/12',
            'focus-visible:border-primary/50 focus-visible:bg-white/6 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
            '[&::-webkit-search-cancel-button]:appearance-none',
            hero ? 'h-14 pr-5 pl-13 text-base' : 'h-11 pr-4 pl-11 text-sm',
          )}
        />
      </div>
      <Button
        type="submit"
        variant="default"
        size={hero ? 'lg' : 'default'}
        disabled={value.trim().length === 0}
        className={hero ? 'h-14 rounded-2xl px-6' : 'rounded-xl'}
      >
        Suchen
      </Button>
    </form>
  )
}

export { SearchField }
