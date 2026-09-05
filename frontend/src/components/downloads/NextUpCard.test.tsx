import { describe, expect, it } from 'bun:test'
import { render, screen } from '@testing-library/react'

import { NextUpCard } from './NextUpCard'
import type { NextUpJob } from '@/types/api'

const sampleNextJobs: NextUpJob[] = [
  {
    job_id: 'job-next-1',
    artist: 'Metallica',
    release: 'Master of Puppets',
    open_tracks: 8,
    total_tracks: 8,
    cover_url: 'https://example.com/cover.jpg',
  },
  {
    job_id: 'job-next-2',
    artist: 'Iron Maiden',
    release: 'The Number of the Beast',
    open_tracks: 1,
    total_tracks: 8,
  },
]

describe('NextUpCard', () => {
  it('renders empty state when queue has no next jobs', () => {
    render(<NextUpCard jobs={[]} />)

    expect(screen.getByText('Keine anstehenden Jobs')).toBeDefined()
    expect(screen.getByText('Keine')).toBeDefined()
  })

  it('renders next-up jobs in exact priority order with track count badges', () => {
    render(<NextUpCard jobs={sampleNextJobs} />)

    expect(screen.getByText('Metallica')).toBeDefined()
    expect(screen.getByText('Master of Puppets')).toBeDefined()
    expect(screen.getByText('8 Tracks offen')).toBeDefined()

    expect(screen.getByText('Iron Maiden')).toBeDefined()
    expect(screen.getByText('The Number of the Beast')).toBeDefined()
    expect(screen.getByText('1 Track offen')).toBeDefined()

    expect(screen.getByText('2 in Vorschau')).toBeDefined()
  })
})
