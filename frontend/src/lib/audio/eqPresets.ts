import type { EQPreset, ParametricFilter } from './types'

export const EQ_FREQUENCIES = [31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000] as const

export const DEFAULT_PARAMETRIC_FILTERS: ParametricFilter[] = [
  { id: 'band-1', enabled: true, type: 'lowshelf', frequency: 80, gain: 0, q: 0.71 },
  { id: 'band-2', enabled: true, type: 'peaking', frequency: 250, gain: 0, q: 1.41 },
  { id: 'band-3', enabled: true, type: 'peaking', frequency: 1000, gain: 0, q: 1.41 },
  { id: 'band-4', enabled: true, type: 'peaking', frequency: 4000, gain: 0, q: 1.41 },
  { id: 'band-5', enabled: true, type: 'highshelf', frequency: 10000, gain: 0, q: 0.71 },
]

export const BUILTIN_PRESETS: EQPreset[] = [
  {
    id: 'flat',
    name: 'Flat',
    values: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
  },
  {
    id: 'bass_boost',
    name: 'Bass Boost',
    values: [5.5, 4.5, 3.5, 2.0, 0.5, 0, 0, 0, 0, 0],
  },
  {
    id: 'bass_reduce',
    name: 'Bass Reduce',
    values: [-5.5, -4.5, -3.5, -2.0, -0.5, 0, 0, 0, 0, 0],
  },
  {
    id: 'treble_boost',
    name: 'Treble Boost',
    values: [0, 0, 0, 0, 0, 0.5, 2.0, 3.5, 4.5, 5.5],
  },
  {
    id: 'treble_reduce',
    name: 'Treble Reduce',
    values: [0, 0, 0, 0, 0, -0.5, -2.0, -3.5, -4.5, -5.5],
  },
  {
    id: 'vocal',
    name: 'Vocal',
    values: [-1.5, -1.0, 0, 2.0, 3.5, 3.0, 2.0, 1.0, 0, -1.0],
  },
  {
    id: 'rock',
    name: 'Rock',
    values: [4.5, 3.0, 1.5, 0, -1.0, -0.5, 1.5, 3.0, 4.0, 4.5],
  },
  {
    id: 'pop',
    name: 'Pop',
    values: [1.5, 2.5, 3.5, 2.0, 0, -1.0, 1.0, 2.5, 3.5, 3.0],
  },
  {
    id: 'electronic',
    name: 'Electronic',
    values: [5.0, 4.0, 2.0, 0, -1.5, 1.0, 0.5, 2.0, 4.0, 4.5],
  },
  {
    id: 'classical',
    name: 'Classical',
    values: [4.0, 3.0, 2.0, 1.0, -0.5, -0.5, 0, 1.5, 2.5, 3.0],
  },
  {
    id: 'acoustic',
    name: 'Acoustic',
    values: [3.0, 2.0, 1.0, 0.5, 1.0, 1.5, 2.0, 2.5, 3.0, 2.5],
  },
]

/**
 * Calculates required auto-headroom reduction in dB to prevent digital clipping
 * when positive EQ boosts are applied.
 */
export function calculateAutoHeadroom(
  bands: number[],
  parametricFilters: ParametricFilter[] = [],
  eqMode: 'graphic' | 'parametric' = 'graphic',
): number {
  let maxGain = 0
  if (eqMode === 'graphic') {
    for (const g of bands) {
      if (g > maxGain) maxGain = g
    }
  } else {
    for (const f of parametricFilters) {
      if (f.enabled && f.gain > maxGain) {
        maxGain = f.gain
      }
    }
  }
  return maxGain > 0 ? -maxGain : 0
}

/** Formats Hz numbers into clean string e.g. 31 Hz, 1 kHz, 16 kHz */
export function formatFrequency(hz: number): string {
  if (hz >= 1000) {
    const k = hz / 1000
    return `${k % 1 === 0 ? k.toFixed(0) : k.toFixed(1)} kHz`
  }
  return `${Math.round(hz)} Hz`
}
