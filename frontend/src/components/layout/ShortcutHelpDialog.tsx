import { Fragment, useEffect, useState } from 'react'

import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { PLAYER_SHORTCUT_HELP } from '@/lib/player-shortcuts'
import type { ShortcutHelpRow } from '@/lib/player-shortcuts'
import { commandShortcutLabel } from '@/lib/shortcut'
import { onShortcutHelpRequest } from '@/lib/shortcut-help'

/**
 * The keyboard shortcut overview. "?" and the command menu entry open it;
 * Escape closes it and Base UI returns the focus to where it was.
 */
export function ShortcutHelpDialog() {
  const [open, setOpen] = useState(false)

  useEffect(() => onShortcutHelpRequest(() => setOpen(true)), [])

  const rows: ShortcutHelpRow[] = [
    ...PLAYER_SHORTCUT_HELP,
    { keys: [commandShortcutLabel()], label: 'Befehlsmenü öffnen' },
    { keys: ['Esc'], label: 'Großen Player schließen' },
  ]

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Tastenkürzel</DialogTitle>
          <DialogDescription>
            Gelten überall in der App, solange kein Eingabefeld, Menü oder Knopf den Fokus hat.
          </DialogDescription>
        </DialogHeader>
        <dl className="grid grid-cols-[auto_1fr] items-center gap-x-6 gap-y-2.5">
          {rows.map((row) => (
            <Fragment key={row.label}>
              <dt className="flex items-center gap-1">
                {row.keys.map((key, index) => (
                  <Fragment key={key}>
                    {index > 0 && (
                      <span className="text-xs text-muted-foreground">
                        {row.joiner === '/' ? (
                          <>
                            <span aria-hidden="true">/</span>
                            <span className="sr-only">oder</span>
                          </>
                        ) : (
                          '+'
                        )}
                      </span>
                    )}
                    <kbd className="inline-flex h-6 min-w-6 items-center justify-center rounded-md border border-border bg-muted px-1.5 font-mono text-[11px] text-foreground">
                      {key}
                    </kbd>
                  </Fragment>
                ))}
              </dt>
              <dd className="text-sm text-muted-foreground">{row.label}</dd>
            </Fragment>
          ))}
        </dl>
      </DialogContent>
    </Dialog>
  )
}
