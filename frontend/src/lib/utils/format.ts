/** Presentation helpers. German output, because the interface is German. */

/** "3:41" — or a dash when the provider delivered no duration. */
export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return '—'

  const totalSeconds = Math.round(ms / 1000)
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60

  const padded = seconds.toString().padStart(2, '0')
  if (hours > 0) {
    return `${hours}:${minutes.toString().padStart(2, '0')}:${padded}`
  }
  return `${minutes}:${padded}`
}

/** "1.234" with German grouping. */
export function formatNumber(value: number): string {
  return new Intl.NumberFormat('de-DE').format(value)
}

/** "13 Tracks" / "1 Track". */
export function pluralize(
  count: number,
  one: string,
  many: string = `${one}s`,
): string {
  return `${formatNumber(count)} ${count === 1 ? one : many}`
}

/** "26. August 2026, 14:12" — or an empty string for a missing timestamp. */
export function formatDateTime(iso: string | undefined): string {
  if (!iso) return ''
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return ''

  return new Intl.DateTimeFormat('de-DE', {
    dateStyle: 'long',
    timeStyle: 'short',
  }).format(date)
}

/** "vor 5 Minuten" — coarse, because exact ages do not matter here. */
export function formatRelative(iso: string | undefined): string {
  if (!iso) return ''
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return ''

  const seconds = Math.round((date.getTime() - Date.now()) / 1000)
  const format = new Intl.RelativeTimeFormat('de-DE', { numeric: 'auto' })

  const steps: [limit: number, divisor: number, unit: Intl.RelativeTimeFormatUnit][] = [
    [60, 1, 'second'],
    [3600, 60, 'minute'],
    [86400, 3600, 'hour'],
    [2592000, 86400, 'day'],
    [31536000, 2592000, 'month'],
  ]

  const magnitude = Math.abs(seconds)
  for (const [limit, divisor, unit] of steps) {
    if (magnitude < limit) return format.format(Math.round(seconds / divisor), unit)
  }
  return format.format(Math.round(seconds / 31536000), 'year')
}

/** "1,4 GB" — used for library size figures the backend reports in bytes. */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'

  const units = ['B', 'kB', 'MB', 'GB', 'TB']
  const exponent = Math.min(
    units.length - 1,
    Math.floor(Math.log(bytes) / Math.log(1000)),
  )
  const value = bytes / 1000 ** exponent
  const digits = exponent === 0 || value >= 100 ? 0 : 1

  return `${new Intl.NumberFormat('de-DE', {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  }).format(value)} ${units[exponent]}`
}

/** Joins credited artists the way the backend tags them. */
export function joinArtists(artists: string[] | undefined): string {
  const cleaned = (artists ?? []).map((name) => name.trim()).filter(Boolean)
  return cleaned.length > 0 ? cleaned.join(' · ') : 'Unbekannter Künstler'
}

/** The year, or an empty string when the provider delivered none. */
export function formatYear(year: number | undefined): string {
  return year && year > 0 ? String(year) : ''
}
