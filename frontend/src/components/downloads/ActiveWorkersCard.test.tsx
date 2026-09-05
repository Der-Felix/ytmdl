import { describe, expect, it } from 'bun:test'
import { render, screen } from '@testing-library/react'

import { ActiveWorkersCard } from './ActiveWorkersCard'
import type { ActiveWorkerPreview } from '@/types/api'

const sampleWorkers: ActiveWorkerPreview[] = [
  {
    job_id: 'job-1',
    item_id: 'item-1',
    artist: 'Green Day',
    release: 'Dookie',
    track: 'Burnout',
    track_number: 1,
    phase: 'downloading',
    progress_percent: 65,
    started_at: '2026-09-05T12:00:00Z',
  },
  {
    job_id: 'job-2',
    item_id: 'item-2',
    artist: 'Linkin Park',
    release: 'Meteora',
    track: 'Numb',
    track_number: 13,
    phase: 'matching',
    progress_percent: 0,
    started_at: '2026-09-05T12:01:00Z',
  },
]

describe('ActiveWorkersCard', () => {
  it('renders empty state when no workers are active', () => {
    render(<ActiveWorkersCard workers={[]} />)

    expect(screen.getByText('Keine Downloads in Bearbeitung')).toBeDefined()
    expect(screen.getByText('0 Worker aktiv')).toBeDefined()
  })

  it('renders active worker details including track number, artist, phase, and progress', () => {
    render(<ActiveWorkersCard workers={sampleWorkers} />)

    expect(screen.getByText('Worker 1')).toBeDefined()
    expect(screen.getByText('Green Day')).toBeDefined()
    expect(screen.getByText('— Dookie')).toBeDefined()
    expect(screen.getByText('01. Burnout')).toBeDefined()
    expect(screen.getByText('65%')).toBeDefined()

    expect(screen.getByText('Worker 2')).toBeDefined()
    expect(screen.getByText('Linkin Park')).toBeDefined()
    expect(screen.getByText('13. Numb')).toBeDefined()
  })

  it('renders 3+ active worker entries correctly without assuming a limit of 2', () => {
    const fourWorkers: ActiveWorkerPreview[] = [
      ...sampleWorkers,
      {
        job_id: 'job-3',
        item_id: 'item-3',
        artist: 'Metallica',
        release: 'Master of Puppets',
        track: 'Battery',
        track_number: 1,
        phase: 'tagging',
        progress_percent: 100,
        started_at: '2026-09-05T12:02:00Z',
      },
      {
        job_id: 'job-4',
        item_id: 'item-4',
        artist: 'Iron Maiden',
        release: 'Powerslave',
        track: 'Aces High',
        track_number: 1,
        phase: 'finalizing',
        progress_percent: 100,
        started_at: '2026-09-05T12:03:00Z',
      },
    ]

    render(<ActiveWorkersCard workers={fourWorkers} />)

    expect(screen.getByText('4 Worker aktiv')).toBeDefined()
    expect(screen.getByText('Worker 1')).toBeDefined()
    expect(screen.getByText('Worker 2')).toBeDefined()
    expect(screen.getByText('Worker 3')).toBeDefined()
    expect(screen.getByText('Worker 4')).toBeDefined()
    expect(screen.getByText('Metallica')).toBeDefined()
    expect(screen.getByText('Iron Maiden')).toBeDefined()
  })
})
