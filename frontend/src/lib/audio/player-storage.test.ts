import { beforeEach, describe, expect, it } from 'bun:test'
import type { EQPreset, PlayerHistoryItem } from './types'
import {
  loadCustomPresets,
  loadHistory,
  loadPlayerState,
  saveCustomPresets,
  saveHistory,
  savePlayerState,
} from './player-storage'

describe('player-storage', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('saves and loads player state scoped by user', () => {
    savePlayerState('user-1', {
      volume: 0.65,
      muted: false,
      shuffle: true,
      crossfadeSeconds: 4,
    })

    savePlayerState('user-2', {
      volume: 0.9,
      muted: true,
      shuffle: false,
      crossfadeSeconds: 0,
    })

    const u1 = loadPlayerState('user-1')
    const u2 = loadPlayerState('user-2')

    expect(u1?.volume).toBe(0.65)
    expect(u1?.shuffle).toBe(true)
    expect(u1?.crossfadeSeconds).toBe(4)

    expect(u2?.volume).toBe(0.9)
    expect(u2?.muted).toBe(true)
    expect(u2?.shuffle).toBe(false)
  })

  it('saves and loads custom presets globally', () => {
    const presets: EQPreset[] = [
      { id: 'custom-1', name: 'My Sound', values: [1, 2, 3, 4, 3, 2, 1, 0, -1, -2], isCustom: true },
    ]
    saveCustomPresets(presets)

    const loaded = loadCustomPresets()
    expect(loaded.length).toBe(1)
    expect(loaded[0].name).toBe('My Sound')
  })

  it('saves and truncates history to max 100 items', () => {
    const history: PlayerHistoryItem[] = Array.from({ length: 120 }, (_, i) => ({
      track: {
        id: `track-${i}`,
        title: `Track ${i}`,
        artists: ['Artist'],
        album: 'Album',
        album_artist: 'Artist',
        track_number: 1,
        track_total: 1,
        disc_number: 1,
        disc_total: 1,
        duration_ms: 1000,
        year: 2024,
        source_provider: 'youtube',
        source_id: 's',
        source_url: 'u',
        created_at: new Date().toISOString(),
      },
      playedAt: new Date().toISOString(),
    }))

    saveHistory('user-1', history)
    const loaded = loadHistory('user-1')
    expect(loaded.length).toBe(100)
    expect(loaded[0].track.id).toBe('track-0')
  })
})
