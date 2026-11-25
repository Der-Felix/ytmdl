import { useEffect, useRef, useState } from 'react'
import type { ComponentType } from 'react'
import { Menu } from '@base-ui/react/menu'
import {
  Disc3,
  Heart,
  HeartOff,
  Info,
  ListEnd,
  ListPlus,
  ListVideo,
  MoreHorizontal,
  Radio,
  UserRound,
} from 'lucide-react'

import { Button } from '@/components/ui/button'
import { useOptionalFavorites } from '@/hooks/useFavorites'
import { usePlayerActions } from '@/hooks/usePlayer'
import { Link, paths, useLocation } from '@/lib/router'
import { cn } from '@/lib/utils'
import type { LibraryTrack } from '@/types/api'
import { AddToPlaylistDialog } from './AddToPlaylistDialog'

/*
 * The one menu for a track, wherever a track row is. It opens from the
 * "more" button and, on a pointer device, from a right-click anywhere in the
 * closest element marked `data-track-row`; a right-click on a link keeps the
 * browser's own menu, so "open in new tab" still works. The keyboard opens it
 * from the button (Enter, Space, arrow down) or with the context menu key.
 *
 * Entries appear only where they apply: no queue entries inside the queue, no
 * radio or playlist for a browser-only copy, no "Zum Album" on the album's
 * own page. Callers add what only they can do (remove from a playlist, move
 * in the queue) as extras at the end.
 */

export interface TrackMenuExtra {
  /** Stable key, also the test id of the entry. */
  id: string
  label: string
  icon: ComponentType<{ className?: string }>
  onSelect: () => void
  disabled?: boolean
  destructive?: boolean
}

interface TrackMenuProps {
  track: LibraryTrack
  /** Inside the queue: the queue entries would only duplicate the title. */
  inQueue?: boolean
  /** Opens the track's details, where the page has a details view. */
  onShowDetails?: () => void
  extras?: TrackMenuExtra[]
  /** Button size: compact in dense desktop tables, 44 px everywhere on phones. */
  size?: 'default' | 'compact'
  className?: string
}

interface VirtualAnchor {
  getBoundingClientRect: () => DOMRect
}

/** A zero-size anchor at the pointer, for a right-click. */
function pointAnchor(x: number, y: number): VirtualAnchor {
  return { getBoundingClientRect: () => DOMRect.fromRect({ x, y, width: 0, height: 0 }) }
}

const itemClass =
  'flex min-h-11 cursor-default items-center gap-3 rounded-lg px-3 text-sm outline-none select-none data-[highlighted]:bg-input data-[disabled]:opacity-40 md:min-h-9'
const iconClass = 'size-4 shrink-0 text-muted-foreground'

export function TrackMenu({
  track,
  inQueue = false,
  onShowDetails,
  extras = [],
  size = 'default',
  className,
}: TrackMenuProps) {
  const { playNext, addToQueue, startRadio } = usePlayerActions()
  const favorites = useOptionalFavorites()
  const { pathname } = useLocation()
  const [open, setOpen] = useState(false)
  const [anchor, setAnchor] = useState<VirtualAnchor | null>(null)
  const [playlistOpen, setPlaylistOpen] = useState(false)
  const [playlistMounted, setPlaylistMounted] = useState(false)
  const triggerRef = useRef<HTMLButtonElement>(null)

  // Right-click on the row this menu belongs to.
  useEffect(() => {
    const row = triggerRef.current?.closest<HTMLElement>('[data-track-row]')
    if (!row) return
    const onContextMenu = (event: MouseEvent) => {
      if ((event as PointerEvent).pointerType === 'touch') return
      const target = event.target as Element | null
      if (target?.closest('a[href], input, textarea, select')) return
      event.preventDefault()
      // The context menu key reports no position; the button anchors then.
      const fromKeyboard = event.button !== 2 || (event.clientX === 0 && event.clientY === 0)
      setAnchor(fromKeyboard ? null : pointAnchor(event.clientX, event.clientY))
      setOpen(true)
    }
    row.addEventListener('contextmenu', onContextMenu)
    return () => row.removeEventListener('contextmenu', onContextMenu)
  }, [])

  const browserOnly = !!track.offline_blob_key
  const artistHref = track.artist_id ? paths.libraryArtist(track.artist_id) : null
  const albumHref = track.release_id ? paths.libraryRelease(track.release_id) : null
  const isFavorite = favorites?.isFavorite(track.id) ?? false

  return (
    <>
      <Menu.Root
        open={open}
        onOpenChange={setOpen}
        onOpenChangeComplete={(next) => {
          if (!next) setAnchor(null)
        }}
      >
        <Menu.Trigger
          ref={triggerRef}
          render={
            <Button
              variant="ghost"
              size="icon"
              className={cn(
                'size-11 shrink-0 data-[popup-open]:bg-secondary data-[popup-open]:text-foreground',
                size === 'compact' && 'md:size-8 md:rounded-lg',
                className,
              )}
            />
          }
          aria-label={`Weitere Aktionen für ${track.title}`}
          title="Weitere Aktionen"
          onClick={(event) => event.stopPropagation()}
          onDoubleClick={(event) => event.stopPropagation()}
        >
          <MoreHorizontal className={cn('size-5', size === 'compact' && 'md:size-4')} aria-hidden="true" />
        </Menu.Trigger>
        <Menu.Portal>
          <Menu.Positioner
            anchor={anchor ?? undefined}
            side="bottom"
            align={anchor ? 'start' : 'end'}
            sideOffset={anchor ? 2 : 6}
            collisionPadding={8}
            className="z-50 outline-none"
          >
            <Menu.Popup
              aria-label={`Aktionen für ${track.title}`}
              className="max-h-(--available-height) w-[min(18rem,calc(100vw-1rem))] origin-(--transform-origin) overflow-y-auto rounded-xl border border-border bg-popover p-1.5 text-popover-foreground shadow-xl outline-none transition-[opacity,scale] duration-(--duration-fast) ease-(--ease-out) data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0 motion-reduce:transition-opacity"
            >
              <p className="truncate px-3 pt-1.5 pb-1 text-xs font-medium text-muted-foreground" aria-hidden="true">
                {track.title}
              </p>
              {!inQueue && (
                <>
                  <Menu.Item className={itemClass} onClick={() => playNext(track)}>
                    <ListEnd className={iconClass} aria-hidden="true" />
                    Als Nächstes abspielen
                  </Menu.Item>
                  <Menu.Item className={itemClass} onClick={() => addToQueue(track)}>
                    <ListPlus className={iconClass} aria-hidden="true" />
                    Zur Warteschlange hinzufügen
                  </Menu.Item>
                </>
              )}
              {!browserOnly && (
                <Menu.Item className={itemClass} onClick={() => void startRadio({ seed: track })}>
                  <Radio className={iconClass} aria-hidden="true" />
                  Song-Radio starten
                </Menu.Item>
              )}
              {!browserOnly && (
                <Menu.Item
                  className={itemClass}
                  onClick={() => {
                    setPlaylistMounted(true)
                    setPlaylistOpen(true)
                  }}
                >
                  <ListVideo className={iconClass} aria-hidden="true" />
                  Zu Playlist hinzufügen …
                </Menu.Item>
              )}
              {favorites && (
                <Menu.Item className={itemClass} onClick={() => void favorites.toggleFavorite(track.id)}>
                  {isFavorite ? (
                    <HeartOff className={iconClass} aria-hidden="true" />
                  ) : (
                    <Heart className={iconClass} aria-hidden="true" />
                  )}
                  {isFavorite ? 'Aus Favoriten entfernen' : 'Zu Favoriten hinzufügen'}
                </Menu.Item>
              )}
              {((artistHref && artistHref !== pathname) || (albumHref && albumHref !== pathname)) && (
                <Menu.Separator className="mx-2 my-1 h-px bg-border" />
              )}
              {artistHref && artistHref !== pathname && (
                <Menu.LinkItem className={itemClass} render={<Link href={artistHref} />}>
                  <UserRound className={iconClass} aria-hidden="true" />
                  Zum Künstler
                </Menu.LinkItem>
              )}
              {albumHref && albumHref !== pathname && (
                <Menu.LinkItem className={itemClass} render={<Link href={albumHref} />}>
                  <Disc3 className={iconClass} aria-hidden="true" />
                  Zum Album
                </Menu.LinkItem>
              )}
              {onShowDetails && (
                <Menu.Item className={itemClass} onClick={onShowDetails}>
                  <Info className={iconClass} aria-hidden="true" />
                  Details anzeigen
                </Menu.Item>
              )}
              {extras.length > 0 && <Menu.Separator className="mx-2 my-1 h-px bg-border" />}
              {extras.map((extra) => (
                <Menu.Item
                  key={extra.id}
                  data-testid={`track-menu-${extra.id}`}
                  className={cn(itemClass, extra.destructive && 'text-destructive')}
                  disabled={extra.disabled}
                  onClick={extra.onSelect}
                >
                  <extra.icon
                    className={cn(iconClass, extra.destructive && 'text-destructive')}
                    aria-hidden="true"
                  />
                  {extra.label}
                </Menu.Item>
              ))}
            </Menu.Popup>
          </Menu.Positioner>
        </Menu.Portal>
      </Menu.Root>
      {playlistMounted && (
        <AddToPlaylistDialog track={track} open={playlistOpen} onOpenChange={setPlaylistOpen} />
      )}
    </>
  )
}
