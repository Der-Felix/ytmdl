// Vertical scroll and small/diagonal gestures never make a destructive choice.
export function duplicateSwipe(dx: number, dy: number): 'left' | 'right' | null {
  if (Math.abs(dx) < 90 || Math.abs(dx) <= Math.abs(dy) * 1.3) return null
  return dx < 0 ? 'left' : 'right'
}

export function deletionSelection(
  winnerID: string,
  memberIDs: string[],
  selectedIDs: string[],
): string[] {
  const members = new Set(memberIDs)
  return [...new Set(selectedIDs)].filter((id) => id !== winnerID && members.has(id))
}

// Unknown/corrupt settings retain the confirmation. Preference is private to this
// browser and account, so another user never inherits direct destructive mode.
const confirmationKey = (userID: string) => `ytmdl.duplicate-review.${userID}.confirmation`
export function readDuplicateConfirmation(userID: string): boolean {
  try {
    return localStorage.getItem(confirmationKey(userID)) !== 'off'
  } catch {
    return true
  }
}
export function writeDuplicateConfirmation(userID: string, ask: boolean): void {
  try {
    localStorage.setItem(confirmationKey(userID), ask ? 'on' : 'off')
  } catch {
    /* Current session still works if storage is unavailable. */
  }
}
