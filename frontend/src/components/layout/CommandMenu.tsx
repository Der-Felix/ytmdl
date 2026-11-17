import { useEffect, useState } from 'react'
import type { ComponentType } from 'react'
import {
  CompassIcon,
  LibraryIcon,
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

import { ADMIN, PRIMARY } from '@/components/layout/navigation'
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
import { usePlayerActions, usePlayerState } from '@/hooks/usePlayer'
import { navigate, paths } from '@/lib/router'

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
 * It renders only inside the signed-in shell and makes no request of its own:
 * every entry navigates or calls a player action.
 */
export function CommandMenu({ open, onOpenChange }: CommandMenuProps) {
  const { isAdmin, logout } = useAuth()
  const { currentTrack, status, shuffle, repeatMode } = usePlayerState()
  const { togglePlayPause, next, previous, toggleShuffle, cycleRepeatMode } = usePlayerActions()
  const [query, setQuery] = useState('')

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

  const views = isAdmin ? [...PRIMARY, ...ADMIN] : PRIMARY
  const trimmed = query.trim()

  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      // Every visit starts with an empty field; cleared once the exit is over.
      onOpenChangeComplete={(isOpen) => {
        if (!isOpen) setQuery('')
      }}
      title="Befehlsmenü"
      description="Ansicht öffnen, suchen oder eine Aktion ausführen"
    >
      <Command loop>
        <CommandInput
          value={query}
          onValueChange={setQuery}
          placeholder="Ansicht, Suche oder Aktion …"
          aria-label="Befehl oder Suchbegriff"
        />
        <CommandList>
          <CommandEmpty>Kein Treffer. Mit Eingabe suchst du in Bibliothek oder Katalog.</CommandEmpty>

          {trimmed && (
            <CommandGroup heading="Suchen" forceMount>
              <CommandItem
                value={`search-library ${trimmed}`}
                forceMount
                onSelect={run(() => navigate(paths.library({ q: trimmed })))}
              >
                <LibraryIcon aria-hidden="true" />
                <span className="min-w-0 truncate">
                  In der Bibliothek suchen: <span className="font-medium">„{trimmed}“</span>
                </span>
              </CommandItem>
              <CommandItem
                value={`search-catalog ${trimmed}`}
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

          <CommandGroup heading="Ansichten">
            {views.map((item) => (
              <MenuItem
                key={item.href}
                icon={item.icon}
                label={item.label}
                keywords={['öffnen', 'gehe zu']}
                onSelect={run(() => navigate(item.href))}
              />
            ))}
          </CommandGroup>

          <CommandGroup heading="Konto">
            <MenuItem icon={UserIcon} label="Profil & Sicherheit" keywords={['passwort', 'konto', 'geräte']} onSelect={run(() => navigate(paths.profile()))} />
            <MenuItem icon={LogOutIcon} label="Abmelden" keywords={['logout', 'sign out']} onSelect={run(() => void logout())} />
          </CommandGroup>
        </CommandList>
        <div className="hidden items-center justify-end gap-3 border-t border-border px-3 py-2 text-[11px] text-muted-foreground pointer-fine:flex">
          <span>
            <CommandShortcut className="ml-0">↑↓</CommandShortcut> auswählen
          </span>
          <span>
            <CommandShortcut className="ml-0">↵</CommandShortcut> ausführen
          </span>
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
