import { describe, expect, it } from 'bun:test'
import { fireEvent, render, screen } from '@testing-library/react'
import { Cover } from './Cover'

describe('artwork fallbacks', () => {
  it('tries local fallback once and resets when the track changes', () => {
    const { rerender, container } = render(<Cover src="/portrait" fallbackSrc="/local-cover" alt="Artist" />)
    fireEvent.error(screen.getByAltText('Artist'))
    expect(screen.getByAltText('Artist').getAttribute('src')).toBe('/local-cover')
    fireEvent.error(screen.getByAltText('Artist'))
    expect(container.querySelector('img')).toBeNull()
    rerender(<Cover src="/next" fallbackSrc="/local-cover" alt="Artist" />)
    expect(screen.getByAltText('Artist').getAttribute('src')).toBe('/next')
  })
  it('does not retry duplicate candidates', () => {
    const { container } = render(<Cover src="/same" fallbackSrc="/same" alt="Cover" />)
    fireEvent.error(screen.getByAltText('Cover'))
    expect(container.querySelector('img')).toBeNull()
  })
})
