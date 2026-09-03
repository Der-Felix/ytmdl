import { describe, expect, it } from 'bun:test'
import { parseLrc } from './lrc'

describe('parseLrc', () => {
  it('parses standard two-digit millisecond timestamps', () => {
    const raw = '[00:12.30]One More Time\n[00:15.50]Celebrate and dance so free'
    const result = parseLrc(raw)
    expect(result.isSynced).toBe(true)
    expect(result.lines).toHaveLength(2)
    expect(result.lines[0].timestamp).toBe('00:12')
    expect(result.lines[0].text).toBe('One More Time')
    expect(result.lines[0].timeSeconds).toBeCloseTo(12.3, 2)
  })

  it('parses three-digit millisecond timestamps', () => {
    const raw = '[01:02.345]Music got me feeling so free'
    const result = parseLrc(raw)
    expect(result.isSynced).toBe(true)
    expect(result.lines[0].timestamp).toBe('01:02')
    expect(result.lines[0].timeSeconds).toBeCloseTo(62.345, 3)
    expect(result.lines[0].text).toBe('Music got me feeling so free')
  })

  it('handles multiple timestamps preceding the same text', () => {
    const raw = '[00:10.00][00:30.00]Chorus line repeated'
    const result = parseLrc(raw)
    expect(result.lines).toHaveLength(2)
    expect(result.lines[0].timestamp).toBe('00:10')
    expect(result.lines[0].text).toBe('Chorus line repeated')
    expect(result.lines[1].timestamp).toBe('00:30')
    expect(result.lines[1].text).toBe('Chorus line repeated')
  })

  it('extracts metadata tags into the metadata map', () => {
    const raw = `[ti:One More Time]
[ar:Daft Punk]
[al:Discovery]
[00:01.00]Intro music`
    const result = parseLrc(raw)
    expect(result.metadata.ti).toBe('One More Time')
    expect(result.metadata.ar).toBe('Daft Punk')
    expect(result.metadata.al).toBe('Discovery')
    expect(result.lines).toHaveLength(1)
  })

  it('safely handles empty lines and broken syntax without crashing', () => {
    const raw = `

[invalid timestamp] Just text
[00:05.10]Valid lyric line
This line has no brackets at all
`
    const result = parseLrc(raw)
    expect(result.isSynced).toBe(true)
    expect(result.lines).toHaveLength(1)
    expect(result.lines[0].text).toBe('Valid lyric line')
  })

  it('correctly parses CRLF line endings', () => {
    const raw = '[00:01.00]Line 1\r\n[00:02.00]Line 2\r\n'
    const result = parseLrc(raw)
    expect(result.lines).toHaveLength(2)
    expect(result.lines[0].text).toBe('Line 1')
    expect(result.lines[1].text).toBe('Line 2')
  })

  it('preserves Unicode characters and emojis', () => {
    const raw = '[00:05.00]日本語の歌詞 · Über den Wolken · Café & 🎵'
    const result = parseLrc(raw)
    expect(result.lines[0].text).toBe('日本語の歌詞 · Über den Wolken · Café & 🎵')
  })

  it('parses offset tag and extended minute timestamps', () => {
    const raw = `[offset:+500]
[al:Live at BBC]
[105:30.50]Encore song`
    const result = parseLrc(raw)
    expect(result.metadata.offset).toBe('+500')
    expect(result.metadata.al).toBe('Live at BBC')
    expect(result.lines).toHaveLength(1)
    expect(result.lines[0].timestamp).toBe('105:30')
    expect(result.lines[0].timeSeconds).toBeCloseTo(105 * 60 + 30.5, 2)
  })

  it('marks plain text without timestamps as unsynced', () => {
    const raw = 'Just a regular plain text lyrics file\nWith multiple lines\nAnd no timestamps at all.'
    const result = parseLrc(raw)
    expect(result.isSynced).toBe(false)
    expect(result.lines).toHaveLength(0)
  })

  it('returns empty result for empty string or nullish input', () => {
    expect(parseLrc('')).toEqual({ metadata: {}, lines: [], isSynced: false })
    // @ts-expect-error test runtime robustness
    expect(parseLrc(null)).toEqual({ metadata: {}, lines: [], isSynced: false })
  })
})
