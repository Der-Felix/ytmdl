import { Repeat, Repeat1, Shuffle } from 'lucide-react'

import { Button } from '@/components/ui/button'
import type { RepeatMode } from '@/lib/audio/types'
import { cn } from '@/lib/utils'

/*
 * Shuffle and repeat, shared by the mini player and the large player. Their
 * state shows three ways: the accent colour, a dot under the icon (so it does
 * not hang on colour alone) and the accessible name or pressed state.
 */

const REPEAT_LABEL: Record<RepeatMode, string> = {
  off: 'Aus',
  queue: 'Warteschlange',
  track: 'Titel',
}

interface ModeButtonProps {
  className?: string
  iconClassName?: string
}

/** The marker under an active mode button. */
function ActiveDot({ active }: { active: boolean }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'pointer-events-none absolute bottom-0.5 left-1/2 size-1 -translate-x-1/2 rounded-full bg-current transition-opacity duration-(--duration-fast)',
        active ? 'opacity-100' : 'opacity-0',
      )}
    />
  )
}

function modeClass(active: boolean) {
  return active ? 'text-primary hover:text-primary/80' : 'text-muted-foreground hover:text-foreground'
}

export function ShuffleButton({
  shuffle,
  onToggle,
  className,
  iconClassName,
}: ModeButtonProps & { shuffle: boolean; onToggle: () => void }) {
  return (
    <Button
      variant="ghost"
      size="icon-sm"
      onClick={onToggle}
      aria-pressed={shuffle}
      aria-label="Zufallswiedergabe"
      title={shuffle ? 'Zufallswiedergabe: Ein' : 'Zufallswiedergabe: Aus'}
      className={cn('relative rounded-full transition-colors', modeClass(shuffle), className)}
    >
      <Shuffle className={cn('size-4', iconClassName)} strokeWidth={1.8} />
      <ActiveDot active={shuffle} />
    </Button>
  )
}

export function RepeatButton({
  mode,
  onCycle,
  className,
  iconClassName,
}: ModeButtonProps & { mode: RepeatMode; onCycle: () => void }) {
  const label = `Wiederholen: ${REPEAT_LABEL[mode]}`
  const Icon = mode === 'track' ? Repeat1 : Repeat
  return (
    <Button
      variant="ghost"
      size="icon-sm"
      onClick={onCycle}
      aria-label={label}
      title={label}
      className={cn('relative rounded-full transition-colors', modeClass(mode !== 'off'), className)}
    >
      <Icon className={cn('size-4', iconClassName)} strokeWidth={1.8} />
      <ActiveDot active={mode !== 'off'} />
    </Button>
  )
}
