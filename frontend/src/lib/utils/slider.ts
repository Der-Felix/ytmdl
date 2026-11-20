import type { CSSProperties } from 'react'

/**
 * Inline style for a `.slider-quiet` range input: how much of the track is
 * filled, and optionally in which colour. The track itself is painted in
 * index.css from these custom properties.
 */
export function sliderFill(value: number, min: number, max: number, color?: string): CSSProperties {
  const span = max - min
  const ratio = span > 0 && Number.isFinite(value) ? (value - min) / span : 0
  const percent = Math.max(0, Math.min(100, ratio * 100))
  return {
    '--slider-fill': `${percent}%`,
    ...(color ? { '--slider-color': color } : {}),
  } as CSSProperties
}
