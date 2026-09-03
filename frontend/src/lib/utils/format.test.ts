import { describe, expect, it } from 'bun:test'
import {
  formatBytes,
  formatDuration,
  formatNumber,
  formatYear,
  joinArtists,
  pluralize,
} from './format'

describe('formatDuration', () => {
  it('formats milliseconds into m:ss', () => {
    expect(formatDuration(0)).toBe('—')
    expect(formatDuration(-100)).toBe('—')
    expect(formatDuration(35000)).toBe('0:35')
    expect(formatDuration(221000)).toBe('3:41')
  })

  it('formats hours into h:mm:ss', () => {
    expect(formatDuration(3661000)).toBe('1:01:01')
    expect(formatDuration(7322000)).toBe('2:02:02')
  })
})

describe('formatNumber & pluralize', () => {
  it('formats numbers with de-DE locale', () => {
    expect(formatNumber(1234)).toBe('1.234')
    expect(formatNumber(10)).toBe('10')
  })

  it('pluralizes nouns with one and many', () => {
    expect(pluralize(1, 'Track')).toBe('1 Track')
    expect(pluralize(5, 'Track')).toBe('5 Tracks')
    expect(pluralize(1, 'Album', 'Alben')).toBe('1 Album')
    expect(pluralize(3, 'Album', 'Alben')).toBe('3 Alben')
  })
})

describe('formatBytes', () => {
  it('formats bytes into human units', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(500)).toBe('500 B')
    expect(formatBytes(1500)).toBe('1,5 kB')
    expect(formatBytes(10485760)).toBe('10,5 MB')
  })
})

describe('joinArtists & formatYear', () => {
  it('joins artist list cleanly', () => {
    expect(joinArtists(['Daft Punk', 'Pharrell Williams'])).toBe('Daft Punk · Pharrell Williams')
    expect(joinArtists([])).toBe('Unbekannter Künstler')
    expect(joinArtists(undefined)).toBe('Unbekannter Künstler')
  })

  it('formats valid year', () => {
    expect(formatYear(2024)).toBe('2024')
    expect(formatYear(0)).toBe('')
    expect(formatYear(undefined)).toBe('')
  })
})
