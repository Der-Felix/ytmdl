/** Listening shorter than this before moving on is not a judgement of the track. */
export const SKIP_MIN_SECONDS = 2
/**
 * Moving on after the track stood paused (or stalled) for longer than this is
 * no judgement of it either: the user left and came back. Three minutes is
 * well past a short interruption and the length of a typical song.
 */
export const SKIP_MAX_PAUSE_SECONDS = 180

/** Counts actual playing wall time, so seeks and buffering never create plays. */
export class ListeningTracker {
  private key = ''
  private last: number | null = null
  private elapsed = 0
  private sent = false
  private skipped = false
  private threshold: number | null = null
  /** When the track was last heard playing (the clock of tick). */
  private heardAt: number | null = null
  reset() {
    this.key = ''
    this.last = null
    this.elapsed = 0
    this.sent = false
    this.skipped = false
    this.threshold = null
    this.heardAt = null
  }
  tick(key: string, playing: boolean, now: number, duration: number, rate = 1): boolean {
    if (key !== this.key) {
      this.reset()
      this.key = key
    }
    if (this.last !== null && playing)
      this.elapsed += Math.max(0, Math.min(1, (now - this.last) / 1000))
    this.last = playing ? now : null
    if (playing) this.heardAt = now
    if (duration > 0) this.threshold = Math.min(30, duration / 2 / Math.max(0.5, rate))
    if (!playing || this.sent || duration <= 0) return false
    if (this.elapsed >= this.threshold!) {
      this.sent = true
      return true
    }
    return false
  }
  /**
   * The user actively moves on from the track `key`. True at most once per
   * track start, and only when at least SKIP_MIN_SECONDS were heard and the
   * track had not yet counted as a play, so the play threshold is the upper
   * bound of a skip. Not while it stood still (not `playing`) for more than
   * SKIP_MAX_PAUSE_SECONDS up to `now` (the clock of tick).
   */
  takeSkip(key: string, now: number, playing: boolean): boolean {
    if (!key || key !== this.key || this.sent || this.skipped) return false
    if (this.elapsed < SKIP_MIN_SECONDS) return false
    if (!playing && (this.heardAt === null || now - this.heardAt > SKIP_MAX_PAUSE_SECONDS * 1000)) return false
    if (this.threshold !== null && this.elapsed >= this.threshold) return false
    this.skipped = true
    return true
  }
}

interface SkipTrack {
  id: string
  offline_blob_key?: string
}

/**
 * Whether a user action that changes the player from `before` to `after`
 * moves on to another track in a way that may be a skip. A track that failed
 * to play, offline copies (they never count as plays either) and staying on
 * the same track (restart, pause, a cleared queue) never are. Natural ends do
 * not pass through here: the tracker is reset when a track ends.
 */
export function movesOnToAnotherTrack(
  before: { currentTrack: SkipTrack | null; status: string; error: string | null },
  after: { currentTrack: SkipTrack | null },
): boolean {
  const from = before.currentTrack,
    to = after.currentTrack
  if (!from || !to || from.offline_blob_key) return false
  if (before.status === 'error' || before.error) return false
  return to.id !== from.id
}
