import { describe, expect, it } from 'bun:test'
import {
  BUILTIN_PRESETS,
  calculateAutoHeadroom,
  EQ_FREQUENCIES,
  formatFrequency,
} from './eqPresets'

describe('EQ Presets & DSP Utils', () => {
  it('defines 10 standard audio frequencies', () => {
    expect(EQ_FREQUENCIES.length).toBe(10)
    expect(EQ_FREQUENCIES[0]).toBe(31)
    expect(EQ_FREQUENCIES[9]).toBe(16000)
  })

  it('provides complete 10-band presets', () => {
    for (const preset of BUILTIN_PRESETS) {
      expect(preset.values.length).toBe(10)
      for (const gain of preset.values) {
        expect(gain).toBeGreaterThanOrEqual(-12)
        expect(gain).toBeLessThanOrEqual(12)
      }
    }
  })

  it('calculates auto headroom attenuation accurately', () => {
    // 1. Flat preset -> 0 headroom reduction
    expect(calculateAutoHeadroom([0, 0, 0, 0, 0, 0, 0, 0, 0, 0])).toBe(0)

    // 2. Bass boost (+5.5 dB max) -> -5.5 dB reduction
    expect(calculateAutoHeadroom([5.5, 4.5, 0, 0, 0, 0, 0, 0, 0, 0])).toBe(-5.5)

    // 3. Only negative gains -> 0 reduction
    expect(calculateAutoHeadroom([-3, -5, -2, 0, 0, 0, 0, 0, 0, 0])).toBe(0)

    // 4. Parametric filter check
    const paramFilters = [
      { id: '1', enabled: true, type: 'peaking' as const, frequency: 1000, gain: 6.0, q: 1.41 },
      { id: '2', enabled: false, type: 'peaking' as const, frequency: 2000, gain: 12.0, q: 1.41 }, // disabled
    ]
    expect(calculateAutoHeadroom([], paramFilters, 'parametric')).toBe(-6.0)
  })

  it('formats frequencies cleanly', () => {
    expect(formatFrequency(31)).toBe('31 Hz')
    expect(formatFrequency(500)).toBe('500 Hz')
    expect(formatFrequency(1000)).toBe('1 kHz')
    expect(formatFrequency(2500)).toBe('2.5 kHz')
    expect(formatFrequency(16000)).toBe('16 kHz')
  })
})
