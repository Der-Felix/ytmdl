import { describe, expect, it } from 'bun:test'

import { sliderFill } from './slider'

const fill = (style: object) => (style as Record<string, string>)['--slider-fill']

describe('sliderFill', () => {
  it('turns a value into the filled share of the track', () => {
    expect(fill(sliderFill(0.25, 0, 1))).toBe('25%')
    expect(fill(sliderFill(90, 0, 180))).toBe('50%')
    expect(fill(sliderFill(6, 2, 10))).toBe('50%')
  })

  it('stays inside the track for values outside the range', () => {
    expect(fill(sliderFill(-5, 0, 10))).toBe('0%')
    expect(fill(sliderFill(50, 0, 10))).toBe('100%')
  })

  it('is empty for a range without length and for a missing value', () => {
    expect(fill(sliderFill(3, 0, 0))).toBe('0%')
    expect(fill(sliderFill(Number.NaN, 0, 10))).toBe('0%')
  })

  it('sets the fill colour only when one is given', () => {
    expect(sliderFill(1, 0, 2)).toEqual({ '--slider-fill': '50%' })
    expect(sliderFill(1, 0, 2, 'var(--muted-foreground)')).toEqual({
      '--slider-fill': '50%',
      '--slider-color': 'var(--muted-foreground)',
    })
  })
})
