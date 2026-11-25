import { useEffect, useRef, useState } from 'react'
import type { ComponentType, KeyboardEvent as ReactKeyboardEvent } from 'react'
import {
  CompassIcon,
  KeyboardIcon,
  LibraryIcon,
  Loader2Icon,
  ListMusicIcon,
  LogOutIcon,
  PauseIcon,
  PlayIcon,
  RepeatIcon,
  ShuffleIcon,
  SkipBackIcon,
  SkipForwardIcon,
  UserIcon,
} from 'lucide-react'

import { ResultGroups } from '@/components/layout/CommandMenuResults'
import { ADMIN, DOWNLOAD_NAV, LISTEN_HOME, LISTEN_NAV } from '@/components/layout/navigation'
import type { NavItem } from '@/components/layout/navigation'
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from '@/components/ui/command'
import { useAuth } from '@/hooks/useAuth'
import { resultScore, useCommandSearch } from '@/hooks/useCommandSearch'
import { usePlayerActions, usePlayerState } from '@/hooks/usePlayer'
import { navigate, paths } from '@/lib/router'
import { openShortcutHelp } from '@/lib/shortcut-help'
import type { LibraryTrack } from '@/types/api'

/** Value prefix of the two search entries; they always rank behind real matches. */
const SEARCH_VALUE = 'search-'

/**
 * Keys the player listens for on the window (see PlayerContext). Inside the
 * menu they belong to the menu: arrows move the selection, letters are typed.
 * They only reach the player when the focus sits on the list rather than the
 * field (after a click into it), so the menu stops them there.
 */
const PLAYER_KEYS = new Set([' ', 'ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight', 'm', 'M', 's', 'S', 'r', 'R'])

/**
 * Ranks an entry for the typed text: prefix of the label, then a contiguous
 * match, then the letters in order (also over the keywords). The search entries
 * are force-mounted, so they only need to rank last: a view, player action or
 * account entry that matches comes first and is the selected one; when nothing
 * else matches, the search entries stand alone. Library results are already
 * matched by the server and keep its order.
 */
function rankEntry(value: string, search: string, keywords?: string[]): number {
  const result = resultScore(value)
  if (result !== undefined) return result
  if (value.startsWith(SEARCH_VALUE)) return 0.0001
  const needle = search.trim().toLowerCase()
  if (!needle) return 1
  const label = value.toLowerCase()
  if (label.startsWith(needle)) return 1
  if (label.includes(needle)) return 0.9
  const haystack = [label, ...(keywords ?? []).map((keyword) => keyword.toLowerCase())]
  // No letters-in-order matching: "dash" would then match "Zufallswiedergabe
  // ausschalten" and, in a group listed earlier, be the entry Enter runs.
  return haystack.some((text) => text.includes(needle)) ? 0.7 : 0
}

interface CommandMenuProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/**
 * The command menu: Ctrl+K (⌘K on a Mac) from anywhere in the signed-in app.
 * It jumps to the main views, hands a search to the library or the catalogue,
 * and runs the player and account actions. Administrative views appear only
 * for administrators, as in the sidebar and the routes.
 *
 * From two typed characters it also shows the user's own library above
 * those entries: titles, artists, albums and playlists. Enter on a title plays
 * it, with the other title results queued behind it; Shift+Enter adds it to
 * the queue and leaves the menu open. Enter on anything else opens its page.
 * Nothing is requested before the menu is open and two characters are typed.
 */
export function CommandMenu({ open, onOpenChange }: CommandMenuProps) {
  const { isAdmin, logout } = useAuth()
  const { currentTrack, status, shuffle, repeatMode } = usePlayerState()
  const { togglePlayPause, next, previous, toggleShuffle, cycleRepeatMode, playTrack, addToQueue } = usePlayerActions()
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState('')
  const search = useCommandSearch(query)
  // True until the user moves the selection: new results may then take it.
  const pristine = useRef(true)

  // Results come in after the typing that asked for them; while the user has
  // not picked anything, the first result is the one Enter runs.
  const { firstValue } = search
  useEffect(() => {
    if (pristine.current && firstValue) setSelected(firstValue)
  }, [firstValue])

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key.toLowerCase() !== 'k' || !(event.metaKey || event.ctrlKey) || event.altKey) return
      event.preventDefault()
      onOpenChange(!open)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [open, onOpenChange])

  const run = (action: () => void) => () => {
    onOpenChange(false)
    action()
  }

  // Every destination, whichever area is open, grouped as the switch groups them.
  const groups: { heading: string; items: NavItem[] }[] = [
    { heading: 'Hören', items: [LISTEN_HOME, ...LISTEN_NAV] },
    { heading: 'Laden', items: DOWNLOAD_NAV },
    ...(isAdmin ? [{ heading: 'Administration', items: ADMIN }] : []),
  ]
  const trimmed = query.trim()

  /** Plays a title result; the other title results follow it in the queue. */
  const playResult = (track: LibraryTrack) => {
    onOpenChange(false)
    if (currentTrack?.id === track.id) {
      if (status !== 'playing') togglePlayPause()
      return
    }
    playTrack(track, [track, ...search.tracks.filter((other) => other.id !== track.id)], 0)
  }

  function onKeyDown(event: ReactKeyboardEvent<HTMLDivElement>) {
    if (event.ctrlKey || event.metaKey || event.altKey) return
    if (PLAYER_KEYS.has(event.key)) event.stopPropagation()
    if (event.key.startsWith('Arrow') || event.key === 'Home' || event.key === 'End') pristine.current = false
    if (event.key === 'Enter' && event.shiftKey) {
      const track = search.trackFor(selected)
      if (!track) return
      // cmdk leaves an event that is already handled alone.
      event.preventDefault()
      addToQueue(track)
    }
  }
  const selectedTrack = search.trackFor(selected)

  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      // Every visit starts with an empty field; cleared once the exit is over.
      onOpenChangeComplete={(isOpen) => {
        if (isOpen) return
        setQuery('')
        setSelected('')
        search.reset()
      }}
      title="Befehlsmenü"
      description="Ansicht öffnen, suchen oder eine Aktion ausführen"
    >
      <Command loop filter={rankEntry} value={selected} onValueChange={setSelected} onKeyDown={onKeyDown}>
        <CommandInput
          value={query}
          onValueChange={(value) => {
            pristine.current = true
            setQuery(value)
            search.update(value)
          }}
          placeholder="Titel, Künstler, Ansicht oder Aktion …"
          aria-label="Befehl oder Suchbegriff"
          trailing={
            // Mounted only while needed: the dialog waits for running
            // animations before it unmounts, and a spinner never finishes.
            // It fades in late, so a quick answer shows no spinner at all.
            open &&
            search.loading && (
              <Loader2Icon
                aria-hidden="true"
                className="size-4 shrink-0 animate-spin text-muted-foreground transition-opacity delay-200 duration-(--duration-fast) starting:opacity-0 motion-reduce:animate-none"
              />
            )
          }
        />
        <p role="status" className="sr-only">
          {search.empty
            ? 'Nichts in deiner Bibliothek gefunden'
            : search.failed
              ? 'Die Bibliothek ist gerade nicht erreichbar'
              : search.settled
                ? `${search.hits} Treffer in deiner Bibliothek`
                : ''}
        </p>
        <CommandList
          onPointerMove={() => {
            pristine.current = false
          }}
        >
          {/* From two characters the library answer below says it instead. */}
          {!search.active && (
            <CommandEmpty>Kein Treffer. Mit Eingabe suchst du in Bibliothek oder Katalog.</CommandEmpty>
          )}

          {(search.empty || search.failed) && (
            <div className="px-2.5 pt-2 pb-1 text-sm">
              <p className="font-medium text-foreground">
                {search.empty ? 'Nichts gefunden' : 'Bibliothek gerade nicht erreichbar'}
              </p>
              <p className="text-xs text-muted-foreground">
                {search.empty
                  ? `Kein Titel, Künstler, Album und keine Playlist passt zu „${trimmed}“. Unten kannst du weitersuchen.`
                  : 'Die Vorschläge fehlen gerade; die Suche unten funktioniert trotzdem.'}
              </p>
            </div>
          )}

          <ResultGroups
            search={search}
            currentTrackId={currentTrack?.id}
            onPlay={playResult}
            onQueue={addToQueue}
            onOpen={(href) => run(() => navigate(href))()}
            onClose={() => onOpenChange(false)}
            hrefs={{ artist: paths.libraryArtist, release: paths.libraryRelease, playlist: paths.playlist }}
          />

          {currentTrack && (
            <CommandGroup heading="Wiedergabe">
              <MenuItem
                icon={status === 'playing' ? PauseIcon : PlayIcon}
                label={status === 'playing' ? 'Pause' : 'Wiedergabe fortsetzen'}
                keywords={['abspielen', 'play', 'pause', 'stop']}
                onSelect={run(togglePlayPause)}
              />
              <MenuItem icon={SkipForwardIcon} label="Nächster Titel" keywords={['weiter', 'next', 'skip']} onSelect={run(() => next(true))} />
              <MenuItem icon={SkipBackIcon} label="Vorheriger Titel" keywords={['zurück', 'previous']} onSelect={run(previous)} />
              <MenuItem
                icon={ShuffleIcon}
                label={shuffle ? 'Zufallswiedergabe ausschalten' : 'Zufallswiedergabe einschalten'}
                keywords={['shuffle', 'zufall', 'mischen']}
                onSelect={run(toggleShuffle)}
              />
              <MenuItem
                icon={RepeatIcon}
                label={`Wiederholen: ${repeatMode === 'off' ? 'Aus' : repeatMode === 'queue' ? 'Warteschlange' : 'Titel'} – umschalten`}
                keywords={['repeat', 'wiederholen', 'schleife']}
                onSelect={run(cycleRepeatMode)}
              />
              <MenuItem
                icon={ListMusicIcon}
                label="Warteschlange öffnen"
                keywords={['queue', 'warteschlange']}
                onSelect={run(() => navigate('/player?tab=queue'))}
              />
            </CommandGroup>
          )}

          {groups.map((group) => (
            <CommandGroup key={group.heading} heading={group.heading}>
              {group.items.map((item) => (
                <MenuItem
                  key={item.href}
                  icon={item.icon}
                  label={item.label}
                  keywords={['öffnen', 'gehe zu', ...(item.keywords ?? [])]}
                  onSelect={run(() => navigate(item.href))}
                />
              ))}
            </CommandGroup>
          ))}

          <CommandGroup heading="Konto">
            <MenuItem icon={UserIcon} label="Profil & Sicherheit" keywords={['passwort', 'konto', 'geräte']} onSelect={run(() => navigate(paths.profile()))} />
            <MenuItem icon={KeyboardIcon} label="Tastenkürzel" keywords={['shortcuts', 'tastatur', 'hilfe']} onSelect={run(openShortcutHelp)} />
            <MenuItem icon={LogOutIcon} label="Abmelden" keywords={['logout', 'sign out']} onSelect={run(() => void logout())} />
          </CommandGroup>

          {trimmed && (
            <CommandGroup heading="Suchen" forceMount>
              <CommandItem
                value={`${SEARCH_VALUE}library ${trimmed}`}
                forceMount
                onSelect={run(() => navigate(paths.library({ q: trimmed })))}
              >
                <LibraryIcon aria-hidden="true" />
                <span className="min-w-0 truncate">
                  In der Bibliothek suchen: <span className="font-medium">„{trimmed}“</span>
                </span>
              </CommandItem>
              <CommandItem
                value={`${SEARCH_VALUE}catalog ${trimmed}`}
                forceMount
                onSelect={run(() => navigate(paths.discover(trimmed)))}
              >
                <CompassIcon aria-hidden="true" />
                <span className="min-w-0 truncate">
                  Im Katalog entdecken: <span className="font-medium">„{trimmed}“</span>
                </span>
              </CommandItem>
            </CommandGroup>
          )}
        </CommandList>
        <div className="hidden items-center justify-end gap-3 border-t border-border px-3 py-2 text-[11px] text-muted-foreground pointer-fine:flex">
          <span>
            <CommandShortcut className="ml-0">↑↓</CommandShortcut> auswählen
          </span>
          {selectedTrack ? (
            <>
              <span>
                <CommandShortcut className="ml-0">↵</CommandShortcut> abspielen
              </span>
              <span>
                <CommandShortcut className="ml-0">⇧↵</CommandShortcut> zur Warteschlange
              </span>
            </>
          ) : (
            <span>
              <CommandShortcut className="ml-0">↵</CommandShortcut> ausführen
            </span>
          )}
          <span>
            <CommandShortcut className="ml-0">Esc</CommandShortcut> schließen
          </span>
        </div>
      </Command>
    </CommandDialog>
  )
}

function MenuItem({
  icon: Icon,
  label,
  keywords,
  onSelect,
}: {
  icon: ComponentType<{ className?: string }>
  label: string
  keywords?: string[]
  onSelect: () => void
}) {
  return (
    <CommandItem value={label} keywords={keywords} onSelect={onSelect}>
      <Icon />
      <span className="min-w-0 truncate">{label}</span>
    </CommandItem>
  )
}
