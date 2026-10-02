/** Counts actual playing wall time, so seeks and buffering never create plays. */
export class ListeningTracker {
  private key = ''
  private last: number | null = null
  private elapsed = 0
  private sent = false
  reset() {
    this.key = ''
    this.last = null
    this.elapsed = 0
    this.sent = false
  }
  tick(key: string, playing: boolean, now: number, duration: number, rate = 1): boolean {
    if (key !== this.key) {
      this.reset()
      this.key = key
    }
    if (this.last !== null && playing)
      this.elapsed += Math.max(0, Math.min(1, (now - this.last) / 1000))
    this.last = playing ? now : null
    if (!playing || this.sent || duration <= 0) return false
    if (this.elapsed >= Math.min(30, duration / 2 / Math.max(0.5, rate))) {
      this.sent = true
      return true
    }
    return false
  }
}
