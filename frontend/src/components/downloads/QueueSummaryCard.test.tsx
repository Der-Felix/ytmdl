import { describe, expect, it } from 'bun:test'
import { render, screen } from '@testing-library/react'

import { QueueSummaryCard } from './QueueSummaryCard'
import type { QueueSummary } from '@/types/api'

const sampleSummary: QueueSummary = {
  active_items: 2,
  remaining_items: 7261,
  paused_jobs: 32,
  retry_wait_items: 0,
  completed_last_hour: 154,
  throughput_items_per_hour: 154.0,
  eta_seconds: 169865,
  eta_confidence: 'high',
  eta_text: 'ca. 2 Tage',
  total_relevant: 7415,
  completed_relevant: 154,
  storage_healthy: true,
  current: [],
  next: [],
}

describe('QueueSummaryCard', () => {
  it('renders open tracks, active items, and throughput correctly', () => {
    render(<QueueSummaryCard summary={sampleSummary} />)

    // Open tracks: 7.261 with de-DE formatting
    expect(screen.getByText('7.261')).toBeDefined()
    expect(screen.getByText('Offene Tracks')).toBeDefined()

    // In Bearbeitung: 2
    expect(screen.getByText('2')).toBeDefined()
    expect(screen.getByText('In Bearbeitung')).toBeDefined()

    // Durchsatz: ~154
    expect(screen.getByText('~154')).toBeDefined()
    expect(screen.getByText('Tracks / Stunde (gemessen)')).toBeDefined()

    // ETA text: ca. 2 Tage
    expect(screen.getByText('ca. 2 Tage')).toBeDefined()
  })

  it('strictly isolates paused jobs as separate count', () => {
    render(<QueueSummaryCard summary={sampleSummary} />)

    expect(screen.getByText('32')).toBeDefined()
    expect(screen.getByText('Pausiert')).toBeDefined()
    expect(screen.getByText('Jobs (separat, nicht in ETA)')).toBeDefined()
  })

  it('displays calculation fallback when throughput is not established', () => {
    const fallbackSummary: QueueSummary = {
      ...sampleSummary,
      throughput_items_per_hour: 0,
      eta_seconds: null,
      eta_confidence: 'none',
      eta_text: 'Berechnung läuft …',
    }
    render(<QueueSummaryCard summary={fallbackSummary} />)

    expect(screen.getAllByText('Berechnung läuft …').length).toBeGreaterThanOrEqual(1)
  })

  it('renders warning banner when storage is unhealthy', () => {
    const unhealthySummary: QueueSummary = {
      ...sampleSummary,
      storage_healthy: false,
      eta_confidence: 'waiting_for_storage',
      eta_text: 'Auf Speicher warten',
    }
    render(<QueueSummaryCard summary={unhealthySummary} />)

    expect(screen.getByText('Auf Speicher warten')).toBeDefined()
    expect(screen.getByText(/Bibliotheksspeicher ist aktuell nicht schreibbar/)).toBeDefined()
  })
})
