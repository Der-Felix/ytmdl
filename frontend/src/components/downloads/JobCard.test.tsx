import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'

import { JobCard } from './JobCard'
import { patchJob } from '@/hooks/useJobs'
import { isWaitingForProvider, section } from '@/lib/api/jobs'
import type { Job, JobDetail, JobItem } from '@/types/api'

function mockJob(overrides: Partial<Job> = {}): Job {
  return {
    id: 'job-1',
    type: 'track',
    status: 'queued',
    priority: 'normal',
    paused: false,
    label: 'Test Track Download',
    metadata_provider: 'deezer',
    media_provider: 'youtube',
    target_id: 'target-1',
    options: {
      release_filter: {
        albums: true,
        singles: true,
        eps: true,
        live: false,
        compilations: false,
        remixes: false,
      },
      skip_existing: true,
    },
    total: 1,
    completed: 0,
    failed: 0,
    skipped: 0,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    ...overrides,
  }
}

function mockItem(overrides: Partial<JobItem> = {}): JobItem {
  return {
    id: 'item-1',
    job_id: 'job-1',
    position: 1,
    status: 'pending',
    label: 'Test Track',
    track: {
      id: 'track-1',
      title: 'In The End',
      artists: ['Linkin Park'],
      album_title: 'Hybrid Theory',
      source_provider: 'deezer',
      source_id: 'src-1',
      source_url: 'https://deezer.com/track/1',
      track_number: 1,
      disc_number: 1,
      duration_ms: 216000,
    },
    match_score: 1.0,
    attempts: 1,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    ...overrides,
  }
}

let originalFetch: typeof fetch

beforeEach(() => {
  originalFetch = globalThis.fetch
})

afterEach(() => {
  cleanup()
  globalThis.fetch = originalFetch
})

describe('JobCard – Step 4 Honest "Waiting for Provider" UX', () => {
  it('1. retry_wait + SESSION_UNAVAILABLE shows "Wartet auf Provider" at job and item levels', async () => {
    const job = mockJob({
      status: 'retry_wait',
      error_code: 'SESSION_UNAVAILABLE',
      error_message: 'Provider vorübergehend nicht verfügbar',
    })
    const item = mockItem({
      status: 'retry_wait',
      error_code: 'SESSION_UNAVAILABLE',
    })

    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('/api/v1/jobs/job-1')) {
        const detail: JobDetail = {
          job,
          items: [item],
          summary: { total: 1, completed: 0, failed: 0, skipped: 0 },
        }
        return new Response(JSON.stringify({ data: detail }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      return new Response('{}', { status: 200 })
    }) as typeof fetch

    render(<JobCard job={job} />)

    // Job-level badge and subtitle show "Wartet auf Provider"
    const badges = screen.getAllByText('Wartet auf Provider')
    expect(badges.length).toBeGreaterThanOrEqual(1)

    // Expand drawer
    const detailsBtn = screen.getByRole('button', { name: /Details/i })
    fireEvent.click(detailsBtn)

    await waitFor(() => {
      // Both job and item badges display "Wartet auf Provider"
      const allWaiting = screen.getAllByText('Wartet auf Provider')
      expect(allWaiting.length).toBeGreaterThanOrEqual(2)
    })
  })

  it('2. retry_wait + SESSION_UNAVAILABLE + future retry time displays automatic continuation time', async () => {
    const now = new Date()
    const retryDate = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 14, 20, 0)
    const retryIso = retryDate.toISOString()

    const job = mockJob({
      status: 'retry_wait',
      error_code: 'SESSION_UNAVAILABLE',
    })
    const item = mockItem({
      status: 'retry_wait',
      error_code: 'SESSION_UNAVAILABLE',
      next_retry_at: retryIso,
    })

    globalThis.fetch = (async () => {
      const detail: JobDetail = {
        job,
        items: [item],
        summary: { total: 1, completed: 0, failed: 0, skipped: 0 },
      }
      return new Response(JSON.stringify({ data: detail }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }) as typeof fetch

    render(<JobCard job={job} />)

    fireEvent.click(screen.getByRole('button', { name: /Details/i }))

    await waitFor(() => {
      expect(screen.getByText(/Automatische Fortsetzung ab 14:20/i)).toBeTruthy()
    })
  })

  it('3. SESSION_UNAVAILABLE is not styled as terminal failure (no text-destructive / red)', async () => {
    const job = mockJob({
      status: 'retry_wait',
      error_code: 'SESSION_UNAVAILABLE',
      error_message: 'Provider vorübergehend nicht verfügbar',
    })
    const item = mockItem({
      status: 'retry_wait',
      error_code: 'SESSION_UNAVAILABLE',
      error_message: 'cooldown active',
    })

    globalThis.fetch = (async () => {
      const detail: JobDetail = {
        job,
        items: [item],
        summary: { total: 1, completed: 0, failed: 0, skipped: 0 },
      }
      return new Response(JSON.stringify({ data: detail }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }) as typeof fetch

    const { container } = render(<JobCard job={job} />)

    // The job-level error message does not use text-destructive
    const errorBox = container.querySelector('p.border-amber-500\\/20')
    expect(errorBox).toBeTruthy()
    expect(errorBox?.classList.contains('text-destructive')).toBe(false)
    expect(errorBox?.classList.contains('text-amber-600')).toBe(true)

    // Expand details
    fireEvent.click(screen.getByRole('button', { name: /Details/i }))

    await waitFor(() => {
      // The item drawer status or message does not have text-destructive
      const itemMsgs = screen.getAllByText('Provider vorübergehend nicht verfügbar', { selector: 'p' })
      expect(itemMsgs.length).toBeGreaterThanOrEqual(2)
      for (const itemMsg of itemMsgs) {
        expect(itemMsg.classList.contains('text-destructive')).toBe(false)
      }
    })
  })

  it('4. SESSION_UNAVAILABLE suppresses immediate retry affordance', async () => {
    const job = mockJob({
      status: 'retry_wait',
      error_code: 'SESSION_UNAVAILABLE',
    })
    const item = mockItem({
      status: 'retry_wait',
      error_code: 'SESSION_UNAVAILABLE',
    })

    globalThis.fetch = (async () => {
      const detail: JobDetail = {
        job,
        items: [item],
        summary: { total: 1, completed: 0, failed: 0, skipped: 0 },
      }
      return new Response(JSON.stringify({ data: detail }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }) as typeof fetch

    render(<JobCard job={job} />)

    fireEvent.click(screen.getByRole('button', { name: /Details/i }))

    await waitFor(() => {
      expect(screen.getAllByText('Wartet auf Provider', { selector: 'span' }).length).toBeGreaterThanOrEqual(2)
    })

    // Immediate retry button for the item is absent
    const retryBtn = screen.queryByTitle('Track jetzt wiederholen')
    expect(retryBtn).toBeNull()
  })

  it('5. unrelated retry_wait preserves existing generic retry presentation and allows retry', async () => {
    const job = mockJob({
      status: 'retry_wait',
      error_code: 'NETWORK_TIMEOUT',
      error_message: 'Netzwerk-Timeout',
    })
    const item = mockItem({
      status: 'retry_wait',
      error_code: 'NETWORK_TIMEOUT',
      error_message: 'Netzwerk-Timeout',
    })

    globalThis.fetch = (async () => {
      const detail: JobDetail = {
        job,
        items: [item],
        summary: { total: 1, completed: 0, failed: 0, skipped: 0 },
      }
      return new Response(JSON.stringify({ data: detail }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }) as typeof fetch

    render(<JobCard job={job} />)

    // Job level badge shows generic "Wartet auf Wiederholung"
    expect(screen.getAllByText('Wartet auf Wiederholung').length).toBeGreaterThanOrEqual(1)

    // Expand details
    fireEvent.click(screen.getByRole('button', { name: /Details/i }))

    await waitFor(() => {
      // Item level badge shows generic "Wiederholung geplant"
      expect(screen.getByText('Wiederholung geplant')).toBeTruthy()
      // Generic retry button is present
      expect(screen.getByTitle('Track jetzt wiederholen')).toBeTruthy()
    })
  })

  it('6. terminal failed preserves existing failed presentation and affords retry', async () => {
    const job = mockJob({
      status: 'failed',
      failed: 1,
      error_code: 'DOWNLOAD_FAILED',
      error_message: 'Der Download ist fehlgeschlagen.',
    })
    const item = mockItem({
      status: 'failed',
      error_code: 'DOWNLOAD_FAILED',
      error_message: 'Der Download ist fehlgeschlagen.',
    })

    globalThis.fetch = (async () => {
      const detail: JobDetail = {
        job,
        items: [item],
        summary: { total: 1, completed: 0, failed: 1, skipped: 0 },
      }
      return new Response(JSON.stringify({ data: detail }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }) as typeof fetch

    render(<JobCard job={job} />)

    // Job badge shows "Fehlgeschlagen"
    expect(screen.getByText('Fehlgeschlagen')).toBeTruthy()
    // "Fehlgeschlagene wiederholen" button is present
    expect(screen.getByRole('button', { name: /Fehlgeschlagene wiederholen/i })).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: /Details/i }))

    await waitFor(() => {
      // Item badge shows "Fehlgeschlagen"
      const itemBadges = screen.getAllByText('Fehlgeschlagen')
      expect(itemBadges.length).toBeGreaterThanOrEqual(2)
      // Track retry button is present
      expect(screen.getByTitle('Track jetzt wiederholen')).toBeTruthy()
    })
  })

  it('7. cancelled and completed jobs remain unchanged', () => {
    const completedJob = mockJob({ status: 'completed', completed: 1 })
    const { unmount } = render(<JobCard job={completedJob} />)
    expect(screen.getByText('Abgeschlossen')).toBeTruthy()
    unmount()

    const cancelledJob = mockJob({ status: 'cancelled' })
    render(<JobCard job={cancelledJob} />)
    expect(screen.getByText('Abgebrochen')).toBeTruthy()
  })

  it('8. Download global counts and section semantics remain unchanged', () => {
    const waitingJob = mockJob({
      status: 'retry_wait',
      error_code: 'SESSION_UNAVAILABLE',
    })
    // In canonical section model, retry_wait belongs to 'active' section, NOT failed
    expect(section(waitingJob)).toBe('active')
    expect(isWaitingForProvider(waitingJob)).toBe(true)

    const failedJob = mockJob({ status: 'failed' })
    expect(section(failedJob)).toBe('failed')
  })

  it('9. recovery SSE event clears waiting state from "Wartet auf Provider" to normal active/downloading UI without refresh', () => {
    const waitingJob = mockJob({
      status: 'retry_wait',
      error_code: 'SESSION_UNAVAILABLE',
      error_message: 'Provider vorübergehend nicht verfügbar',
    })

    const { rerender } = render(<JobCard job={waitingJob} />)

    // Verify initial waiting state is shown correctly
    expect(screen.getAllByText('Wartet auf Provider').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('Provider vorübergehend nicht verfügbar')).toBeTruthy()

    // Receive recovery SSE event
    const recoveryEvent: JobEvent = {
      type: 'job.status',
      time: new Date().toISOString(),
      job_id: waitingJob.id,
      status: 'downloading',
      error_code: '',
      error_message: '',
    }

    // Client state is updated from SSE stream without reload
    const recoveredJob = patchJob(waitingJob, recoveryEvent)
    expect(recoveredJob.error_code).toBeUndefined()
    expect(recoveredJob.error_message).toBeUndefined()

    // Rerender component with live patched state
    rerender(<JobCard job={recoveredJob} />)

    // Verify waiting label and amber banner disappeared without refresh
    expect(screen.queryByText('Wartet auf Provider')).toBeNull()
    expect(screen.queryByText('Provider vorübergehend nicht verfügbar')).toBeNull()
    expect(screen.getAllByText('Wird heruntergeladen').length).toBeGreaterThanOrEqual(1)
  })
})
