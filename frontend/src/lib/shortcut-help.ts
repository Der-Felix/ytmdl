/**
 * Opens the keyboard shortcut overview from anywhere: the "?" key in the
 * player and the command menu entry both call this. The overview listens only
 * while the signed-in shell shows it, so a request elsewhere does nothing.
 */

const OPEN_EVENT = 'ytmdl:shortcut-help'

export function openShortcutHelp(): void {
  window.dispatchEvent(new Event(OPEN_EVENT))
}

export function onShortcutHelpRequest(listener: () => void): () => void {
  window.addEventListener(OPEN_EVENT, listener)
  return () => window.removeEventListener(OPEN_EVENT, listener)
}
