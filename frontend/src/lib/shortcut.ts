/** The command menu shortcut as the user's keyboard labels it: ⌘K on a Mac, Strg K elsewhere. */
export function commandShortcutLabel(): string {
  if (typeof navigator === 'undefined') return 'Strg K'
  const platform =
    (navigator as Navigator & { userAgentData?: { platform?: string } }).userAgentData?.platform ??
    navigator.platform ??
    ''
  return /mac|iphone|ipad/i.test(platform) ? '⌘K' : 'Strg K'
}
