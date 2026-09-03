export interface LrcLine {
  timeSeconds: number
  timestamp: string // formatted mm:ss
  text: string
}

export interface ParsedLrc {
  metadata: Record<string, string>
  lines: LrcLine[]
  isSynced: boolean
}

const TIMESTAMP_REGEX = /\[(\d{1,3}):(\d{2})(?:\.(\d{1,3}))?\]/g
const TAG_REGEX = /^\[([a-zA-Z]+):(.*)\]$/

export function parseLrc(raw: string): ParsedLrc {
  if (!raw || typeof raw !== 'string') {
    return { metadata: {}, lines: [], isSynced: false }
  }

  const lines = raw.split(/\r?\n/)
  const metadata: Record<string, string> = {}
  const parsedLines: LrcLine[] = []

  for (const rawLine of lines) {
    const trimmed = rawLine.trim()
    if (!trimmed) continue

    // Check for ID tags like [ar:Artist], [ti:Title]
    const tagMatch = trimmed.match(TAG_REGEX)
    if (tagMatch && tagMatch[1] && tagMatch[2] !== undefined) {
      const key = tagMatch[1].toLowerCase()
      const val = tagMatch[2].trim()
      metadata[key] = val
      continue
    }

    // Extract all timestamps from the line
    const timestamps: { timeSeconds: number; timestamp: string }[] = []
    let match: RegExpExecArray | null
    let lastIndex = 0
    TIMESTAMP_REGEX.lastIndex = 0

    while ((match = TIMESTAMP_REGEX.exec(trimmed)) !== null) {
      if (!match[1] || !match[2]) continue
      const minutes = parseInt(match[1], 10)
      const seconds = parseInt(match[2], 10)
      const fractionStr = match[3] || '0'
      const fraction = parseFloat(`0.${fractionStr}`)
      const timeSeconds = minutes * 60 + seconds + fraction

      const pad = (n: number) => n.toString().padStart(2, '0')
      const formatted = `${pad(minutes)}:${pad(seconds)}`

      timestamps.push({ timeSeconds, timestamp: formatted })
      lastIndex = TIMESTAMP_REGEX.lastIndex
    }

    if (timestamps.length > 0) {
      const text = trimmed.slice(lastIndex).trim()
      for (const ts of timestamps) {
        parsedLines.push({
          timeSeconds: ts.timeSeconds,
          timestamp: ts.timestamp,
          text,
        })
      }
    }
  }

  // Sort chronologically
  parsedLines.sort((a, b) => a.timeSeconds - b.timeSeconds)

  return {
    metadata,
    lines: parsedLines,
    isSynced: parsedLines.length > 0,
  }
}
