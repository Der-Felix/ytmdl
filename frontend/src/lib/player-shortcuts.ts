/**
 * The player's global keyboard shortcuts: which key does what, and when a key
 * is not ours to take. The decision is a pure function so it can be tested
 * without a player; PlayerContext only carries the result out.
 */

export type PlayerShortcut =
  | 'togglePlay'
  | 'seekBackward'
  | 'seekForward'
  | 'previous'
  | 'next'
  | 'volumeUp'
  | 'volumeDown'
  | 'toggleMute'
  | 'toggleShuffle'
  | 'cycleRepeat'
  | 'showHelp'

/**
 * Elements that use these keys themselves: Space presses a button or opens a
 * summary, arrows move through menus, lists, tabs and sliders, and dialogs
 * and the command menu own every key while they have focus.
 */
const OWN_KEYS_SELECTOR = [
  'button',
  'a[href]',
  'summary',
  '[role=button]',
  '[role=menu]',
  '[role=menuitem]',
  '[role=listbox]',
  '[role=option]',
  '[role=slider]',
  '[role=tab]',
  '[role=tablist]',
  '[role=dialog]',
  '[role=alertdialog]',
  '[role=combobox]',
  '[cmdk-root]',
].join(',')

type ShortcutEvent = Pick<
  KeyboardEvent,
  'key' | 'shiftKey' | 'ctrlKey' | 'metaKey' | 'altKey' | 'repeat' | 'defaultPrevented'
> & { target: EventTarget | null }

function isElement(target: EventTarget | null): target is Element {
  return typeof Element !== 'undefined' && target instanceof Element
}

/** True when the focused element types text or handles the keys itself. */
export function targetOwnsKeys(target: EventTarget | null): boolean {
  if (!isElement(target)) return false
  if (target instanceof HTMLElement && target.isContentEditable) return true
  if (target.closest('input, textarea, select, [contenteditable=""], [contenteditable="true"]')) return true
  return target.closest(OWN_KEYS_SELECTOR) !== null
}

/**
 * The shortcut a key press triggers, or null when the press belongs to the
 * page: the caller prevents the default only for a non-null result, so plain
 * ↑/↓ keep scrolling and a focused button keeps its Space.
 */
export function playerShortcutFor(event: ShortcutEvent): PlayerShortcut | null {
  if (event.defaultPrevented) return null
  // Ctrl, ⌘ and Alt belong to the browser and the system (reload, AltGr …).
  if (event.ctrlKey || event.metaKey || event.altKey) return null
  if (targetOwnsKeys(event.target)) return null

  const shift = event.shiftKey
  switch (event.key) {
    case ' ':
      // Holding Space would toggle playback on every repeat.
      if (shift || event.repeat) return null
      return 'togglePlay'
    case 'ArrowLeft':
      return shift ? 'previous' : 'seekBackward'
    case 'ArrowRight':
      return shift ? 'next' : 'seekForward'
    case 'ArrowUp':
      // Plain ↑/↓ scroll the page.
      return shift ? 'volumeUp' : null
    case 'ArrowDown':
      return shift ? 'volumeDown' : null
    case 'm':
    case 'M':
      return shift ? null : 'toggleMute'
    case 's':
    case 'S':
      return shift ? null : 'toggleShuffle'
    case 'r':
    case 'R':
      return shift ? null : 'cycleRepeat'
    case '?':
      // Shift is part of "?" on most layouts (Shift+ß on German ones).
      return 'showHelp'
    default:
      return null
  }
}

export interface ShortcutHelpRow {
  keys: string[]
  /** How the keys combine: pressed together (+) or alternatives (/). */
  joiner?: '+' | '/'
  label: string
}

/** The list the shortcut overview shows, in German, in reading order. */
export const PLAYER_SHORTCUT_HELP: ShortcutHelpRow[] = [
  { keys: ['Leertaste'], label: 'Wiedergabe / Pause' },
  { keys: ['←', '→'], joiner: '/', label: '5 Sekunden zurück / vor' },
  { keys: ['Umschalt', '←'], label: 'Vorheriger Titel' },
  { keys: ['Umschalt', '→'], label: 'Nächster Titel' },
  { keys: ['Umschalt', '↑'], label: 'Lauter' },
  { keys: ['Umschalt', '↓'], label: 'Leiser' },
  { keys: ['M'], label: 'Stummschalten' },
  { keys: ['S'], label: 'Zufallswiedergabe' },
  { keys: ['R'], label: 'Wiederholen: Aus, Warteschlange, Titel' },
  { keys: ['?'], label: 'Diese Übersicht' },
]
