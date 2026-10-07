import { Menu } from '@base-ui/react/menu'
import { ArrowDown, ArrowUp, ListPlus, MoreHorizontal, Play, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
export function PlaylistTrackMenu({ title, moveUp, moveDown, next, enqueue, remove, busy, smart }: {
  title: string; moveUp?: () => void; moveDown?: () => void; next: () => void; enqueue: () => void; remove: () => void; busy: boolean; smart: boolean
}) {
  const item = 'flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm outline-none data-[highlighted]:bg-white/10 data-[disabled]:opacity-35'
  return <Menu.Root>
    <Menu.Trigger render={<Button variant="ghost" size="icon" aria-label={`Aktionen für ${title}`} className="size-11 shrink-0" />}><MoreHorizontal className="size-5" /></Menu.Trigger>
    <Menu.Portal><Menu.Positioner side="bottom" align="end" sideOffset={6} className="z-50"><Menu.Popup className="min-w-60 rounded-xl border border-border bg-popover p-1.5 text-popover-foreground shadow-xl">
      <Menu.Item className={item} onClick={next}><Play className="size-4" />Als Nächstes abspielen</Menu.Item>
      <Menu.Item className={item} onClick={enqueue}><ListPlus className="size-4" />Zur Warteschlange hinzufügen</Menu.Item>
      {!smart && <>
        <Menu.Item className={item} disabled={busy || !moveUp} onClick={moveUp}><ArrowUp className="size-4" />Nach oben verschieben</Menu.Item>
        <Menu.Item className={item} disabled={busy || !moveDown} onClick={moveDown}><ArrowDown className="size-4" />Nach unten verschieben</Menu.Item>
        <Menu.Item className={`${item} text-destructive`} disabled={busy} onClick={remove}><Trash2 className="size-4" />Aus Playlist entfernen</Menu.Item>
      </>}
    </Menu.Popup></Menu.Positioner></Menu.Portal>
  </Menu.Root>
}
